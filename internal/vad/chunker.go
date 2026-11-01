// Package vad splits a live audio stream into chunks at natural pauses so each chunk can be
// transcribed in the background while the user keeps talking. Pure Go, no platform code.
package vad

import "math"

// Config tunes the chunker. These are internal constants, not user options (PLAN.md §4).
type Config struct {
	SampleRate    int     // samples per second (16000)
	FrameMs       int     // analysis frame length
	PauseMs       int     // silence that ends a chunk
	MinChunkMs    int     // don't cut chunks shorter than this at a pause
	MaxChunkMs    int     // force a cut before Whisper's 30 s window
	MinSpeechMs   int     // chunks with less speech than this are marked as silence
	MinRMS        float64 // absolute speech threshold floor
	NoiseFactor   float64 // speech = louder than noise floor × this
	NoiseWindowMs int     // noise floor = quietest frame within this window
}

// DefaultConfig returns the tuned defaults for 16 kHz input.
func DefaultConfig() Config {
	return Config{
		SampleRate:    16000,
		FrameMs:       30,
		PauseMs:       600,
		MinChunkMs:    2000,
		MaxChunkMs:    25000,
		MinSpeechMs:   300,
		MinRMS:        0.004,
		NoiseFactor:   3.0,
		NoiseWindowMs: 2000,
	}
}

// Chunk is a span of audio ready for transcription.
type Chunk struct {
	Samples   []float32
	HasSpeech bool  // false = essentially silence; callers should skip it
	StartMs   int64 // offset from the start of the session
}

// Chunker accumulates samples and emits chunks. Not safe for concurrent use.
type Chunker struct {
	cfg      Config
	frameLen int

	pending []float32 // incomplete frame
	cur     []float32 // samples of the chunk being built
	speech  []bool    // per complete frame in cur
	rms     []float64 // per complete frame in cur

	silentRun    int // consecutive silent frames at the end of cur
	speechFrames int // speech frames in cur
	trailing     int // consecutive silent frames across chunk boundaries

	history []float64 // ring of recent frame RMS values; its minimum is the noise floor
	histPos int

	consumed int64 // samples already emitted or dropped (start offset of cur)
}

// New creates a chunker.
func New(cfg Config) *Chunker {
	return &Chunker{cfg: cfg, frameLen: cfg.SampleRate * cfg.FrameMs / 1000}
}

func (c *Chunker) ms(frames int) int { return frames * c.cfg.FrameMs }

// Write feeds samples and returns any chunks completed by them.
func (c *Chunker) Write(samples []float32) []Chunk {
	var out []Chunk
	c.pending = append(c.pending, samples...)
	for len(c.pending) >= c.frameLen {
		frame := c.pending[:c.frameLen]
		if ch, ok := c.frame(frame); ok {
			out = append(out, ch)
		}
		c.pending = c.pending[c.frameLen:]
	}
	// keep pending from pinning a large backing array
	if cap(c.pending) > 4*c.frameLen {
		c.pending = append([]float32(nil), c.pending...)
	}
	return out
}

// Flush emits whatever is buffered (including a partial frame) as a final chunk.
func (c *Chunker) Flush() (Chunk, bool) {
	c.cur = append(c.cur, c.pending...)
	c.pending = nil
	if len(c.cur) == 0 {
		return Chunk{}, false
	}
	return c.emit(len(c.cur)), true
}

// TrailingSilenceMs is how long the input has been silent, across chunk boundaries.
func (c *Chunker) TrailingSilenceMs() int { return c.ms(c.trailing) }

func (c *Chunker) frame(frame []float32) (Chunk, bool) {
	var sum float64
	for _, s := range frame {
		sum += float64(s) * float64(s)
	}
	r := math.Sqrt(sum / float64(len(frame)))

	isSpeech := r > math.Max(c.cfg.MinRMS, c.noiseFloor(r)*c.cfg.NoiseFactor)

	c.cur = append(c.cur, frame...)
	c.speech = append(c.speech, isSpeech)
	c.rms = append(c.rms, r)
	if isSpeech {
		c.silentRun, c.trailing = 0, 0
		c.speechFrames++
	} else {
		c.silentRun++
		c.trailing++
	}

	frames := len(c.speech)
	switch {
	case c.speechFrames == 0 && c.ms(frames) >= c.cfg.PauseMs:
		// Nothing said yet: drop old silence, keep a short lead-in so a word onset isn't clipped.
		keep := 300 / c.cfg.FrameMs
		c.drop(frames - keep)
	case c.ms(frames) >= c.cfg.MinChunkMs && c.ms(c.silentRun) >= c.cfg.PauseMs:
		return c.emit(len(c.cur)), true
	case c.ms(frames) >= c.cfg.MaxChunkMs:
		// No pause long enough: cut after the quietest frame in the last third.
		best := frames * 2 / 3
		for i := best; i < frames; i++ {
			if c.rms[i] < c.rms[best] {
				best = i
			}
		}
		return c.emit((best + 1) * c.frameLen), true
	}
	return Chunk{}, false
}

// emit returns the first n samples of cur as a chunk and keeps the rest.
func (c *Chunker) emit(n int) Chunk {
	framesOut := n / c.frameLen
	if framesOut > len(c.speech) {
		framesOut = len(c.speech)
	}
	speechOut := 0
	for _, s := range c.speech[:framesOut] {
		if s {
			speechOut++
		}
	}
	ch := Chunk{
		Samples:   append([]float32(nil), c.cur[:n]...),
		HasSpeech: c.ms(speechOut) >= c.cfg.MinSpeechMs,
		StartMs:   c.consumed * 1000 / int64(c.cfg.SampleRate),
	}
	c.consumed += int64(n)
	c.cur = append([]float32(nil), c.cur[n:]...)
	c.speech = append([]bool(nil), c.speech[framesOut:]...)
	c.rms = append([]float64(nil), c.rms[framesOut:]...)
	c.recount()
	return ch
}

// drop discards the first n frames of cur (leading silence).
func (c *Chunker) drop(n int) {
	if n <= 0 {
		return
	}
	c.consumed += int64(n * c.frameLen)
	c.cur = append([]float32(nil), c.cur[n*c.frameLen:]...)
	c.speech = append([]bool(nil), c.speech[n:]...)
	c.rms = append([]float64(nil), c.rms[n:]...)
	c.recount()
}

// noiseFloor records r and returns the quietest frame RMS of the last NoiseWindowMs. Speech dips
// between syllables, so this tracks the room's background level, including a noisy room.
func (c *Chunker) noiseFloor(r float64) float64 {
	n := c.cfg.NoiseWindowMs / c.cfg.FrameMs
	if n < 1 {
		n = 1
	}
	if len(c.history) < n {
		c.history = append(c.history, r)
	} else {
		c.history[c.histPos] = r
		c.histPos = (c.histPos + 1) % n
	}
	floor := c.history[0]
	for _, v := range c.history[1:] {
		floor = math.Min(floor, v)
	}
	return floor
}

func (c *Chunker) recount() {
	c.speechFrames, c.silentRun = 0, 0
	for _, s := range c.speech {
		if s {
			c.speechFrames++
			c.silentRun = 0
		} else {
			c.silentRun++
		}
	}
}
