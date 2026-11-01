package app

import (
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brian-nunez/bvoice/internal/audio"
	"github.com/brian-nunez/bvoice/internal/history"
	"github.com/brian-nunez/bvoice/internal/platform"
	"github.com/brian-nunez/bvoice/internal/settings"
	"github.com/brian-nunez/bvoice/internal/ui"
)

type fakeRecorder struct {
	mu      sync.Mutex
	cb      func([]float32)
	device  string
	failErr error
}

func (r *fakeRecorder) Devices() ([]audio.Device, error) {
	return []audio.Device{{Name: "Built-in Mic", Default: true}, {Name: "USB Mic"}}, nil
}

func (r *fakeRecorder) Start(device string, cb func([]float32)) error {
	if r.failErr != nil {
		return r.failErr
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.cb, r.device = cb, device

	return nil
}

func (r *fakeRecorder) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.cb = nil

	return nil
}

// feed delivers audio in 10 ms callbacks, like a real device; it stops early if recording stops.
func (r *fakeRecorder) feed(samples []float32) {
	for i := 0; i < len(samples); i += 160 {
		r.mu.Lock()

		cb := r.cb
		if cb != nil {
			cb(samples[i:min(i+160, len(samples))])
		}
		r.mu.Unlock()

		if cb == nil {
			return
		}
	}
}

type fakeModel struct {
	mu      sync.Mutex
	calls   int
	prompts []string
}

func (m *fakeModel) Transcribe(_ []float32, prompt string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls++
	m.prompts = append(m.prompts, prompt)

	return fmt.Sprintf(" Part %d. [BLANK_AUDIO]", m.calls), nil
}

func (m *fakeModel) Calls() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return m.calls
}

type fakeStore struct {
	mu       sync.Mutex
	settings map[string]string
	entries  []history.Entry
	nextID   int64
}

func newFakeStore() *fakeStore { return &fakeStore{settings: map[string]string{}} }

func (s *fakeStore) LoadSettings() (map[string]string, error) { return s.settings, nil }
func (s *fakeStore) SaveSettings(m map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for k, v := range m {
		s.settings[k] = v
	}

	return nil
}

func (s *fakeStore) AddTranscription(e history.Entry, limit int) (int64, error) {
	s.mu.Lock()
	s.nextID++
	e.ID = s.nextID
	s.entries = append(s.entries, e)
	s.mu.Unlock()

	return e.ID, s.Prune(limit)
}

func (s *fakeStore) Prune(limit int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.entries) > limit {
		s.entries = s.entries[len(s.entries)-limit:]
	}

	return nil
}

func (s *fakeStore) History(q string, limit int) ([]history.Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []history.Entry
	for i := len(s.entries) - 1; i >= 0 && len(out) < limit; i-- {
		if strings.Contains(strings.ToLower(s.entries[i].Text), strings.ToLower(q)) {
			out = append(out, s.entries[i])
		}
	}

	return out, nil
}

func (s *fakeStore) Get(id int64) (history.Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for _, e := range s.entries {
		if e.ID == id {
			return e, nil
		}
	}

	return history.Entry{}, errors.New("not found")
}

func (s *fakeStore) ClearHistory() error {
	s.mu.Lock()
	s.entries = nil
	s.mu.Unlock()

	return nil
}

func (s *fakeStore) ExportMarkdown(w io.Writer) error {
	_, err := io.WriteString(w, "# bvoice history\n")

	return err
}

type fakePlatform struct {
	mu     sync.Mutex
	clip   []string
	sounds []platform.Sound
	key    string
	onTap  func()
	closed bool
}

func (p *fakePlatform) Watch(key string, onTap func()) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.key, p.onTap = key, onTap

	return nil
}

func (p *fakePlatform) SetKey(key string) error {
	p.mu.Lock()
	p.key = key
	p.mu.Unlock()

	return nil
}
func (p *fakePlatform) Close() { p.mu.Lock(); p.closed = true; p.mu.Unlock() }
func (p *fakePlatform) SetText(t string) error {
	p.mu.Lock()
	p.clip = append(p.clip, t)
	p.mu.Unlock()

	return nil
}

func (p *fakePlatform) Play(s platform.Sound, _ float64) {
	p.mu.Lock()
	p.sounds = append(p.sounds, s)
	p.mu.Unlock()
}

func (p *fakePlatform) Clip() []string {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]string(nil), p.clip...)
}

func (p *fakePlatform) Sounds() []platform.Sound {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([]platform.Sound(nil), p.sounds...)
}

type fakeUI struct {
	mu       sync.Mutex
	status   ui.Status
	statuses []ui.Phase
	history  []ui.HistoryItem
	controls []ui.Control
	notice   []settings.Change
	onTop    bool
	stopped  bool
}

func (u *fakeUI) Run(_ ui.Actions, _ bool, _ func()) error { return nil }
func (u *fakeUI) SetStatus(s ui.Status) {
	u.mu.Lock()
	u.status = s
	u.statuses = append(u.statuses, s.Phase)
	u.mu.Unlock()
}
func (u *fakeUI) SetHistory(h []ui.HistoryItem)      { u.mu.Lock(); u.history = h; u.mu.Unlock() }
func (u *fakeUI) SetControls(c []ui.Control)         { u.mu.Lock(); u.controls = c; u.mu.Unlock() }
func (u *fakeUI) SetSyncNotice(ch []settings.Change) { u.mu.Lock(); u.notice = ch; u.mu.Unlock() }
func (u *fakeUI) SetOnTop(b bool)                    { u.mu.Lock(); u.onTop = b; u.mu.Unlock() }
func (u *fakeUI) Stop()                              { u.mu.Lock(); u.stopped = true; u.mu.Unlock() }

func (u *fakeUI) Status() ui.Status {
	u.mu.Lock()
	defer u.mu.Unlock()

	return u.status
}

func (u *fakeUI) Notice() []settings.Change {
	u.mu.Lock()
	defer u.mu.Unlock()

	return u.notice
}

func (u *fakeUI) History() []ui.HistoryItem {
	u.mu.Lock()
	defer u.mu.Unlock()

	return u.history
}

func (u *fakeUI) Controls() []ui.Control {
	u.mu.Lock()
	defer u.mu.Unlock()

	return u.controls
}

const rate = audio.SampleRate

func speech(ms int) []float32 {
	out := make([]float32, rate*ms/1000)
	for i := range out {
		t := float64(i) / rate
		out[i] = float32(0.3 * math.Abs(math.Sin(2*math.Pi*3*t)) * math.Sin(2*math.Pi*220*t))
	}

	return out
}

func silence(ms int) []float32 { return make([]float32, rate*ms/1000) }

type rig struct {
	c     *Controller
	rec   *fakeRecorder
	model *fakeModel
	store *fakeStore
	plat  *fakePlatform
	ui    *fakeUI
}

func newRig(t *testing.T, saved settings.Settings, ov settings.Overrides) *rig {
	t.Helper()

	r := &rig{rec: &fakeRecorder{}, model: &fakeModel{}, store: newFakeStore(), plat: &fakePlatform{}, ui: &fakeUI{}}

	r.c = New(Config{
		Model: r.model, ModelName: "test-model", Version: "v0.0.0-test",
		Recorder: r.rec, Store: r.store,
		Platform: platform.Services{Hotkey: r.plat, Clipboard: r.plat, Sounds: r.plat},
		UI:       r.ui, Saved: saved, Overrides: ov,
		Log: log.New(io.Discard, "", 0),
	})
	if err := r.c.Start(); err != nil {
		t.Fatal(err)
	}

	return r
}

func waitPhase(t *testing.T, u *fakeUI, p ui.Phase) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if u.Status().Phase == p {
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("phase stayed %q, want %q", u.Status().Phase, p)
}

func TestDictationEndToEnd(t *testing.T) {
	r := newRig(t, settings.Defaults(), nil)
	if r.plat.key != "left_option" || r.plat.onTap == nil {
		t.Fatalf("hotkey not started: %q", r.plat.key)
	}

	r.plat.onTap() // tap: start
	waitPhase(t, r.ui, ui.Listening)
	// three sentences with pauses; the first two should be transcribed while still "talking"
	r.rec.feed(speech(3000))
	r.rec.feed(silence(1000))
	r.rec.feed(speech(3000))
	r.rec.feed(silence(1000))

	deadline := time.Now().Add(2 * time.Second)
	for r.model.Calls() < 2 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	if got := r.model.Calls(); got != 2 {
		t.Fatalf("expected 2 chunks transcribed in the background before stopping, got %d", got)
	}

	r.rec.feed(speech(2000))

	r.plat.onTap() // tap: stop
	waitPhase(t, r.ui, ui.Idle)

	clip := r.plat.Clip()
	if len(clip) != 1 || clip[0] != "Part 1. Part 2. Part 3. " {
		t.Fatalf("clipboard = %q", clip)
	}

	if s := r.plat.Sounds(); len(s) != 2 || s[0] != platform.SoundStart || s[1] != platform.SoundDone {
		t.Fatalf("sounds = %v", s)
	}

	if len(r.store.entries) != 1 || r.store.entries[0].Text != "Part 1. Part 2. Part 3." || r.store.entries[0].Model != "test-model" {
		t.Fatalf("history = %+v", r.store.entries)
	}

	if a := r.store.entries[0].AudioMs; a < 9900 || a > 10100 {
		t.Fatalf("audio length = %d ms, want ~10000", a)
	}

	if h := r.ui.History(); len(h) != 1 || !strings.Contains(h[0].Meta, "test-model · 0:10") {
		t.Fatalf("ui history = %+v", h)
	}
	// later chunks are primed with the earlier text
	if p := r.model.prompts[2]; !strings.Contains(p, "Part 1. Part 2.") {
		t.Fatalf("third chunk prompt = %q", p)
	}
}

func TestVocabularyPrimesPrompt(t *testing.T) {
	saved := settings.Defaults()
	saved.Vocabulary = "Kubernetes, PostgreSQL"
	r := newRig(t, saved, nil)
	r.c.ToggleRecording()
	r.rec.feed(speech(2500))
	r.c.ToggleRecording()
	waitPhase(t, r.ui, ui.Idle)

	if len(r.model.prompts) != 1 || !strings.HasPrefix(r.model.prompts[0], "Kubernetes, PostgreSQL") {
		t.Fatalf("prompts = %q", r.model.prompts)
	}
}

func TestSilenceOnlyCopiesNothing(t *testing.T) {
	r := newRig(t, settings.Defaults(), nil)
	r.c.ToggleRecording()
	r.rec.feed(silence(4000))
	r.c.ToggleRecording()
	waitPhase(t, r.ui, ui.Idle)

	if r.model.Calls() != 0 || len(r.plat.Clip()) != 0 || len(r.store.entries) != 0 {
		t.Fatalf("silence should not be transcribed or saved")
	}

	if msg := r.ui.Status().Message; msg != "No speech detected." {
		t.Fatalf("message = %q", msg)
	}
}

func TestAutoStopAfterSilence(t *testing.T) {
	saved := settings.Defaults()
	saved.AutoStopSec = 2
	r := newRig(t, saved, nil)
	r.c.ToggleRecording()
	r.rec.feed(speech(2500))
	r.rec.feed(silence(5000)) // feeding stops once the recorder is stopped
	waitPhase(t, r.ui, ui.Idle)

	if clip := r.plat.Clip(); len(clip) != 1 {
		t.Fatalf("auto-stop should still deliver the text, clipboard = %q", clip)
	}

	if msg := r.ui.Status().Message; !strings.Contains(msg, "silence") {
		t.Fatalf("message = %q", msg)
	}
}

func TestSoundsOffAutoCopyOffNoTrailingSpace(t *testing.T) {
	saved := settings.Defaults()
	saved.Sounds, saved.AutoCopy, saved.TrailingSpace = false, false, false
	r := newRig(t, saved, nil)
	r.c.ToggleRecording()
	r.rec.feed(speech(2500))
	r.c.ToggleRecording()
	waitPhase(t, r.ui, ui.Idle)

	if len(r.plat.Sounds()) != 0 || len(r.plat.Clip()) != 0 {
		t.Fatalf("sounds=%v clip=%v", r.plat.Sounds(), r.plat.Clip())
	}

	if len(r.store.entries) != 1 {
		t.Fatal("history should still be saved")
	}

	r.c.CopyEntry(r.store.entries[0].ID)

	if clip := r.plat.Clip(); len(clip) != 1 || clip[0] != "Part 1." {
		t.Fatalf("copy from history = %q", clip)
	}
}

func TestMicFailure(t *testing.T) {
	r := newRig(t, settings.Defaults(), nil)
	r.rec.failErr = errors.New("permission denied")
	r.c.ToggleRecording()
	waitPhase(t, r.ui, ui.Idle)

	if msg := r.ui.Status().Message; !strings.Contains(msg, "microphone") {
		t.Fatalf("message = %q", msg)
	}

	if s := r.plat.Sounds(); len(s) != 1 || s[0] != platform.SoundError {
		t.Fatalf("sounds = %v", s)
	}
}

func TestFlagsNoticeSyncAndUIChanges(t *testing.T) {
	saved := settings.Defaults()
	saved.Sounds = false
	r := newRig(t, saved, settings.Overrides{"sounds": "true", "max-minutes": "45", "on-top": "true"})

	if !r.c.Settings().Sounds || r.c.Settings().MaxMinutes != 45 {
		t.Fatalf("flags should apply for this run: %+v", r.c.Settings())
	}

	if n := r.ui.Notice(); len(n) != 2 { // on-top equals the saved value
		t.Fatalf("notice = %+v", n)
	}

	if len(r.store.settings) != 0 {
		t.Fatal("flags must not be saved automatically")
	}

	// changing a flagged option in the UI saves it and removes it from the notice
	r.c.SetSetting("max-minutes", "60")

	if r.store.settings["max-minutes"] != "60" || r.c.Settings().MaxMinutes != 60 {
		t.Fatalf("UI change not saved/applied: %v", r.store.settings)
	}

	if n := r.ui.Notice(); len(n) != 1 || n[0].Key != "sounds" {
		t.Fatalf("notice after UI change = %+v", n)
	}

	// Sync saves the remaining flag values and hides the notice
	r.c.SyncFlags()

	if r.store.settings["sounds"] != "true" || len(r.ui.Notice()) != 0 {
		t.Fatalf("sync: settings=%v notice=%v", r.store.settings, r.ui.Notice())
	}

	// invalid values are rejected and reported
	r.c.SetSetting("sound-volume", "7")

	if !strings.Contains(r.ui.Status().Message, "sound-volume") || r.c.Settings().SoundVolume != 0.5 {
		t.Fatalf("invalid value accepted: %+v / %q", r.c.Settings(), r.ui.Status().Message)
	}

	// hotkey and on-top apply live
	r.c.SetSetting("hotkey", "right_command")
	r.c.SetSetting("on-top", "false")

	if r.plat.key != "right_command" || r.ui.onTop {
		t.Fatalf("live apply failed: key=%q onTop=%v", r.plat.key, r.ui.onTop)
	}
}

func TestControlsAndHistorySearch(t *testing.T) {
	r := newRig(t, settings.Defaults(), nil)

	ctls := r.ui.Controls()
	if len(ctls) != len(settings.Fields) {
		t.Fatalf("controls = %d, fields = %d", len(ctls), len(settings.Fields))
	}

	byKey := map[string]ui.Control{}
	for _, c := range ctls {
		byKey[c.Key] = c
	}

	if mic := byKey["mic"]; len(mic.Choices) != 3 || mic.Choices[0].Label != "System default" {
		t.Fatalf("mic choices = %+v", mic.Choices)
	}

	if hk := byKey["hotkey"]; hk.Value != "left_option" || hk.Choices[0].Label != "Left Option" {
		t.Fatalf("hotkey control = %+v", hk)
	}

	for _, txt := range []string{"deploy notes", "grocery list", "Deploy again"} {
		if _, err := r.store.AddTranscription(history.Entry{Text: txt, Model: "m", CreatedAt: time.Now()}, 100); err != nil {
			t.Fatal(err)
		}
	}

	r.c.Search("deploy")
	h := r.ui.History()
	sort.Slice(h, func(i, j int) bool { return h[i].ID < h[j].ID })

	if len(h) != 2 || h[0].Preview != "deploy notes" {
		t.Fatalf("search = %+v", h)
	}

	r.c.ClearHistory()

	if len(r.ui.History()) != 0 {
		t.Fatal("clear did not refresh the list")
	}
}

func TestQuitFinishesDictation(t *testing.T) {
	r := newRig(t, settings.Defaults(), nil)
	r.c.ToggleRecording()
	r.rec.feed(speech(2500))
	r.c.Quit()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r.ui.mu.Lock()
		stopped := r.ui.stopped
		r.ui.mu.Unlock()

		if stopped {
			break
		}

		time.Sleep(5 * time.Millisecond)
	}

	if len(r.store.entries) != 1 || !r.plat.closed {
		t.Fatalf("quit should save the dictation in progress and release the hotkey")
	}
}

func TestHotkeyLabel(t *testing.T) {
	for in, want := range map[string]string{"left_option": "Left Option", "fn": "Fn", "right_command": "Right Command"} {
		if got := HotkeyLabel(in); got != want {
			t.Errorf("HotkeyLabel(%q) = %q", in, got)
		}
	}
}
