// Package app is bvoice's controller: the dictation state machine and the glue between audio,
// transcription, storage, platform services and the UI. It only talks to interfaces, so it is
// platform-independent and tested with fakes.
package app

import (
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/brian-nunez/bvoice/internal/audio"
	"github.com/brian-nunez/bvoice/internal/history"
	"github.com/brian-nunez/bvoice/internal/platform"
	"github.com/brian-nunez/bvoice/internal/settings"
	"github.com/brian-nunez/bvoice/internal/textproc"
	"github.com/brian-nunez/bvoice/internal/ui"
	"github.com/brian-nunez/bvoice/internal/vad"
)

// Transcriber turns 16 kHz mono samples into text (whisper.Model implements it).
type Transcriber interface {
	Transcribe(samples []float32, prompt string) (string, error)
}

// Store is the persistence the controller needs (store.Store implements it).
type Store interface {
	LoadSettings() (map[string]string, error)
	SaveSettings(map[string]string) error
	AddTranscription(e history.Entry, limit int) (int64, error)
	Prune(limit int) error
	History(query string, limit int) ([]history.Entry, error)
	Get(id int64) (history.Entry, error)
	ClearHistory() error
	ExportMarkdown(w io.Writer) error
}

// Config wires the controller.
type Config struct {
	Model     Transcriber
	ModelName string
	Version   string
	Recorder  audio.Recorder
	Store     Store
	Platform  platform.Services
	UI        ui.UI
	Saved     settings.Settings  // settings loaded from the database
	Overrides settings.Overrides // flags passed for this run
	Log       *log.Logger
}

// Controller implements ui.Actions.
type Controller struct {
	cfg Config
	log *log.Logger

	mu        sync.Mutex
	saved     settings.Settings
	eff       settings.Settings // saved + overrides (+ UI changes)
	overrides settings.Overrides
	phase     ui.Phase
	sess      *session
	message   string
	query     string
}

var _ ui.Actions = (*Controller)(nil)

// New creates a controller.
func New(cfg Config) *Controller {
	l := cfg.Log
	if l == nil {
		l = log.New(os.Stderr, "bvoice: ", log.LstdFlags)
	}

	ov := settings.Overrides{}
	for k, v := range cfg.Overrides {
		ov[k] = v
	}

	return &Controller{
		cfg:       cfg,
		log:       l,
		saved:     cfg.Saved,
		eff:       settings.Apply(cfg.Saved, ov),
		overrides: ov,
		phase:     ui.Idle,
	}
}

// Settings returns the effective settings.
func (c *Controller) Settings() settings.Settings {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.eff
}

// Start pushes the initial state to the UI and starts the hotkey. Call it from the UI's ready callback.
func (c *Controller) Start() error {
	c.mu.Lock()
	eff := c.eff
	c.mu.Unlock()
	c.cfg.UI.SetOnTop(eff.OnTop)
	c.pushStatus()
	c.pushControls()
	c.pushNotice()
	c.refreshHistory()

	if err := c.cfg.Platform.Hotkey.Watch(eff.Hotkey, c.ToggleRecording); err != nil {
		return fmt.Errorf("hotkey: %w", err)
	}

	return nil
}

// ToggleRecording starts listening when idle and stops when listening. Ignored while finishing.
func (c *Controller) ToggleRecording() {
	c.mu.Lock()
	phase := c.phase
	c.mu.Unlock()

	switch phase {
	case ui.Idle:
		c.startListening()
	case ui.Listening:
		go c.stopListening("")
	}
}

type session struct {
	started time.Time
	chunker *vad.Chunker
	work    chan vad.Chunk
	done    chan struct{}

	mu      sync.Mutex // guards chunker + samples (audio thread vs. stop)
	samples int64
	closed  bool

	parts    []string // worker-only until done is closed
	stopping atomic.Bool
}

func (c *Controller) startListening() {
	c.mu.Lock()
	if c.phase != ui.Idle {
		c.mu.Unlock()

		return
	}

	eff := c.eff
	s := &session{
		started: time.Now(),
		chunker: vad.New(vad.DefaultConfig()),
		work:    make(chan vad.Chunk, 1024),
		done:    make(chan struct{}),
	}
	c.sess = s
	c.phase = ui.Listening
	c.message = ""
	c.mu.Unlock()

	go c.worker(s, eff.Vocabulary)

	autoStopMs := eff.AutoStopSec * 1000
	maxSamples := int64(eff.MaxMinutes) * 60 * audio.SampleRate

	err := c.cfg.Recorder.Start(eff.Mic, func(in []float32) {
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()

			return
		}

		s.samples += int64(len(in))
		for _, ch := range s.chunker.Write(in) {
			if ch.HasSpeech {
				s.work <- ch // buffered; the worker drains it while the user talks
			}
		}

		silent := s.chunker.TrailingSilenceMs()
		total := s.samples
		s.mu.Unlock()

		switch {
		case autoStopMs > 0 && silent >= autoStopMs:
			c.autoStop(s, fmt.Sprintf("Stopped after %ds of silence.", eff.AutoStopSec))
		case total >= maxSamples:
			c.autoStop(s, fmt.Sprintf("Stopped at the %d-minute limit.", eff.MaxMinutes))
		}
	})
	if err != nil {
		c.log.Printf("start recording: %v", err)
		close(s.work)
		<-s.done
		c.mu.Lock()
		c.phase, c.sess = ui.Idle, nil
		c.message = "Could not open the microphone: " + err.Error()
		c.mu.Unlock()
		c.play(platform.SoundError)
		c.pushStatus()

		return
	}

	c.play(platform.SoundStart)
	c.pushStatus()
	c.log.Printf("listening (mic %q)", eff.Mic)
}

func (c *Controller) autoStop(s *session, reason string) {
	if s.stopping.CompareAndSwap(false, true) {
		go c.finish(s, reason)
	}
}

func (c *Controller) stopListening(reason string) {
	c.mu.Lock()
	s := c.sess
	c.mu.Unlock()

	if s != nil && s.stopping.CompareAndSwap(false, true) {
		c.finish(s, reason)
	}
}

// worker transcribes chunks in order while the user keeps talking.
func (c *Controller) worker(s *session, vocab string) {
	defer close(s.done)

	var sofar string

	for ch := range s.work {
		t0 := time.Now()

		raw, err := c.cfg.Model.Transcribe(ch.Samples, textproc.Prompt(vocab, sofar))
		if err != nil {
			c.log.Printf("transcribe chunk at %dms: %v", ch.StartMs, err)

			continue
		}

		if txt := textproc.Clean(raw); txt != "" {
			s.parts = append(s.parts, txt)
			sofar = strings.Join(s.parts, " ")
		}

		c.log.Printf("chunk %.1fs at %dms transcribed in %v", float64(len(ch.Samples))/audio.SampleRate, ch.StartMs, time.Since(t0).Round(time.Millisecond))
	}
}

func (c *Controller) finish(s *session, reason string) {
	stopAt := time.Now()

	c.mu.Lock()
	c.phase = ui.Finishing
	eff := c.eff
	c.mu.Unlock()
	c.pushStatus()

	if err := c.cfg.Recorder.Stop(); err != nil {
		c.log.Printf("stop recording: %v", err)
	}

	s.mu.Lock()

	s.closed = true // late audio callbacks (if any) are ignored from here on
	if last, ok := s.chunker.Flush(); ok && last.HasSpeech {
		s.work <- last
	}

	audioMs := s.samples * 1000 / audio.SampleRate
	close(s.work)
	s.mu.Unlock()
	<-s.done

	text := textproc.Join(s.parts)
	latency := time.Since(stopAt)

	msg := reason
	if text == "" {
		if msg == "" {
			msg = "No speech detected."
		}
	} else {
		out := text
		if eff.TrailingSpace {
			out += " "
		}

		if eff.AutoCopy {
			if err := c.cfg.Platform.Clipboard.SetText(out); err != nil {
				c.log.Printf("clipboard: %v", err)

				msg = "Could not copy to the clipboard."
			}
		}

		_, err := c.cfg.Store.AddTranscription(history.Entry{
			CreatedAt: s.started, Model: c.cfg.ModelName, AudioMs: audioMs,
			LatencyMs: latency.Milliseconds(), Text: text,
		}, eff.HistoryLimit)
		if err != nil {
			c.log.Printf("save history: %v", err)
		}

		c.log.Printf("done: %d chars from %.1fs of audio, ready %v after stop", len(text), float64(audioMs)/1000, latency.Round(time.Millisecond))
	}

	c.mu.Lock()
	c.phase, c.sess, c.message = ui.Idle, nil, msg
	c.mu.Unlock()

	if text != "" {
		c.play(platform.SoundDone)
	}

	c.pushStatus()
	c.refreshHistory()
}

func (c *Controller) play(s platform.Sound) {
	c.mu.Lock()
	eff := c.eff
	c.mu.Unlock()

	if eff.Sounds {
		c.cfg.Platform.Sounds.Play(s, eff.SoundVolume)
	}
}

// SetSetting saves a change made in the UI and applies it immediately.
func (c *Controller) SetSetting(key, value string) {
	c.mu.Lock()

	saved, eff := c.saved, c.eff
	if err := saved.Set(key, value); err != nil {
		c.message = err.Error()
		c.mu.Unlock()
		c.pushStatus()
		c.pushControls() // revert the control to the effective value

		return
	}

	_ = eff.Set(key, value)
	oldEff := c.eff
	c.saved, c.eff = saved, eff
	delete(c.overrides, key) // the UI value now wins for this key
	c.message = ""
	c.mu.Unlock()

	if err := c.cfg.Store.SaveSettings(map[string]string{key: mustGet(saved, key)}); err != nil {
		c.log.Printf("save setting %s: %v", key, err)
	}

	switch key {
	case "hotkey":
		if err := c.cfg.Platform.Hotkey.SetKey(eff.Hotkey); err != nil {
			c.log.Printf("hotkey: %v", err)
		}

		c.pushStatus()
	case "on-top":
		c.cfg.UI.SetOnTop(eff.OnTop)
	case "history-limit":
		if eff.HistoryLimit < oldEff.HistoryLimit {
			if err := c.cfg.Store.Prune(eff.HistoryLimit); err != nil {
				c.log.Printf("prune: %v", err)
			}

			c.refreshHistory()
		}
	}

	c.pushNotice()
}

// SyncFlags saves this run's flag values as the saved settings.
func (c *Controller) SyncFlags() {
	c.mu.Lock()
	c.saved = settings.Apply(c.saved, c.overrides)
	c.overrides = settings.Overrides{}
	saved := c.saved
	c.mu.Unlock()

	if err := c.cfg.Store.SaveSettings(saved.ToMap()); err != nil {
		c.log.Printf("sync settings: %v", err)
	}

	c.pushNotice()
}

func mustGet(s settings.Settings, key string) string {
	v, _ := s.Get(key)

	return v
}

// Search filters the history list.
func (c *Controller) Search(query string) {
	c.mu.Lock()
	c.query = query
	c.mu.Unlock()
	c.refreshHistory()
}

// CopyEntry copies a past transcription to the clipboard.
func (c *Controller) CopyEntry(id int64) {
	e, err := c.cfg.Store.Get(id)
	if err != nil {
		c.log.Printf("copy entry %d: %v", id, err)

		return
	}

	c.mu.Lock()
	trailing := c.eff.TrailingSpace
	c.mu.Unlock()

	text := e.Text
	if trailing {
		text += " "
	}

	msg := "Copied to the clipboard."
	if err := c.cfg.Platform.Clipboard.SetText(text); err != nil {
		msg = "Could not copy to the clipboard."
	}

	c.mu.Lock()
	c.message = msg
	c.mu.Unlock()
	c.pushStatus()
}

// Export writes the history as Markdown to path (chosen by the user in a save dialog).
func (c *Controller) Export(path string) {
	f, err := os.Create(path)
	if err == nil {
		err = c.cfg.Store.ExportMarkdown(f)
		if cerr := f.Close(); err == nil {
			err = cerr
		}
	}

	msg := "History exported."

	if err != nil {
		c.log.Printf("export: %v", err)
		msg = "Export failed: " + err.Error()
	}

	c.mu.Lock()
	c.message = msg
	c.mu.Unlock()
	c.pushStatus()
}

// ClearHistory deletes all saved transcriptions (the UI confirms first).
func (c *Controller) ClearHistory() {
	if err := c.cfg.Store.ClearHistory(); err != nil {
		c.log.Printf("clear history: %v", err)
	}

	c.refreshHistory()
}

// Quit finishes any dictation in progress (so nothing is lost), then stops the UI.
func (c *Controller) Quit() {
	go func() {
		c.stopListening("")
		c.cfg.Platform.Hotkey.Close()
		c.cfg.UI.Stop()
	}()
}

const historyRows = 200

func (c *Controller) refreshHistory() {
	c.mu.Lock()
	q := c.query
	c.mu.Unlock()

	entries, err := c.cfg.Store.History(q, historyRows)
	if err != nil {
		c.log.Printf("load history: %v", err)

		return
	}

	items := make([]ui.HistoryItem, 0, len(entries))
	for _, e := range entries {
		items = append(items, HistoryItem(e))
	}

	c.cfg.UI.SetHistory(items)
}

// HistoryItem formats an entry for the list.
func HistoryItem(e history.Entry) ui.HistoryItem {
	secs := e.AudioMs / 1000
	meta := fmt.Sprintf("%s · %d:%02d · ready in %.1fs", e.Model, secs/60, secs%60, float64(e.LatencyMs)/1000)

	preview := strings.Join(strings.Fields(e.Text), " ")
	if r := []rune(preview); len(r) > 140 {
		preview = string(r[:140]) + "…"
	}

	return ui.HistoryItem{ID: e.ID, Time: e.CreatedAt.Format("Jan 2 15:04"), Meta: meta, Preview: preview}
}

func (c *Controller) pushStatus() {
	c.mu.Lock()

	st := ui.Status{
		Phase: c.phase, Model: c.cfg.ModelName, Version: c.cfg.Version,
		Hotkey: c.eff.Hotkey, Message: c.message,
	}
	if c.sess != nil {
		st.StartedAtMs = c.sess.started.UnixMilli()
	}
	c.mu.Unlock()
	c.cfg.UI.SetStatus(st)
}

func (c *Controller) pushNotice() {
	c.mu.Lock()
	diff := settings.Diff(c.saved, c.overrides)
	c.mu.Unlock()
	c.cfg.UI.SetSyncNotice(diff)
}

func (c *Controller) pushControls() {
	c.mu.Lock()
	eff := c.eff
	c.mu.Unlock()

	mics := []ui.Choice{{Value: "", Label: "System default"}}

	if devs, err := c.cfg.Recorder.Devices(); err == nil {
		for _, d := range devs {
			mics = append(mics, ui.Choice{Value: d.Name, Label: d.Name})
		}
	} else {
		c.log.Printf("list microphones: %v", err)
	}

	controls := make([]ui.Control, 0, len(settings.Fields))
	for _, f := range settings.Fields {
		ctl := ui.Control{Key: f.Key, Label: f.Label, Help: f.Help, Kind: f.Kind, Value: mustGet(eff, f.Key), Min: f.Min, Max: f.Max}
		switch f.Key {
		case "mic":
			ctl.Choices = mics
		case "hotkey":
			for _, h := range f.Choices {
				ctl.Choices = append(ctl.Choices, ui.Choice{Value: h, Label: HotkeyLabel(h)})
			}
		default:
			for _, ch := range f.Choices {
				ctl.Choices = append(ctl.Choices, ui.Choice{Value: ch, Label: ch})
			}
		}

		controls = append(controls, ctl)
	}

	c.cfg.UI.SetControls(controls)
}

// HotkeyLabel turns "left_option" into "Left Option".
func HotkeyLabel(h string) string {
	if h == "fn" {
		return "Fn"
	}

	words := strings.Split(h, "_")
	for i, w := range words {
		if w != "" {
			words[i] = strings.ToUpper(w[:1]) + w[1:]
		}
	}

	return strings.Join(words, " ")
}
