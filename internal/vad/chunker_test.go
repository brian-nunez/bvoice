package vad

import (
	"math"
	"math/rand"
	"testing"
)

const rate = 16000

// speech is a syllable-like signal: a 220 Hz tone whose loudness rises and falls ~6 times a second.
func speech(ms int) []float32 {
	n := rate * ms / 1000

	out := make([]float32, n)
	for i := range out {
		t := float64(i) / rate
		out[i] = float32(0.3 * math.Abs(math.Sin(2*math.Pi*3*t)) * math.Sin(2*math.Pi*220*t))
	}

	return out
}

func noise(ms int, level float64) []float32 {
	r := rand.New(rand.NewSource(int64(ms)))

	out := make([]float32, rate*ms/1000)
	for i := range out {
		out[i] = float32((r.Float64()*2 - 1) * level)
	}

	return out
}

func silence(ms int) []float32 { return noise(ms, 0.001) }

func add(a, b []float32) []float32 {
	out := append([]float32(nil), a...)
	for i := range out {
		if i < len(b) {
			out[i] += b[i]
		}
	}

	return out
}

func concat(parts ...[]float32) []float32 {
	var out []float32
	for _, p := range parts {
		out = append(out, p...)
	}

	return out
}

// feed writes in 10 ms pieces, like an audio callback, and collects every chunk including the flush.
func feed(c *Chunker, audio []float32) []Chunk {
	var out []Chunk

	step := rate / 100
	for i := 0; i < len(audio); i += step {
		end := min(i+step, len(audio))
		out = append(out, c.Write(audio[i:end])...)
	}

	if ch, ok := c.Flush(); ok {
		out = append(out, ch)
	}

	return out
}

func speechChunks(chs []Chunk) []Chunk {
	var out []Chunk

	for _, c := range chs {
		if c.HasSpeech {
			out = append(out, c)
		}
	}

	return out
}

func TestCutsAtPause(t *testing.T) {
	c := New(DefaultConfig())

	var early []Chunk

	early = append(early, c.Write(speech(3000))...)

	early = append(early, c.Write(silence(1000))...)
	if len(speechChunks(early)) != 1 {
		t.Fatalf("expected the first sentence to be emitted at the pause, before flush; got %d", len(early))
	}

	rest := feed(c, speech(3000))
	if got := len(speechChunks(append(early, rest...))); got != 2 {
		t.Fatalf("want 2 speech chunks, got %d", got)
	}
}

func TestShortPauseDoesNotCut(t *testing.T) {
	c := New(DefaultConfig())

	chs := feed(c, concat(speech(1500), silence(300), speech(1500)))
	if got := len(speechChunks(chs)); got != 1 {
		t.Fatalf("a 300 ms pause should not split; got %d speech chunks", got)
	}
}

func TestForcedCutOnLongSpeech(t *testing.T) {
	cfg := DefaultConfig()
	c := New(cfg)
	audio := speech(60000)

	chs := feed(c, audio)
	if len(chs) < 3 {
		t.Fatalf("60 s of unbroken speech should give >= 3 chunks, got %d", len(chs))
	}

	total := 0

	for _, ch := range chs {
		if len(ch.Samples) > rate*cfg.MaxChunkMs/1000 {
			t.Fatalf("chunk of %d samples exceeds the max chunk length", len(ch.Samples))
		}

		total += len(ch.Samples)
	}

	if total != len(audio) {
		t.Fatalf("samples not conserved: %d in, %d out", len(audio), total)
	}
}

func TestOffsetsAreContiguous(t *testing.T) {
	c := New(DefaultConfig())
	chs := feed(c, concat(speech(3000), silence(1000), speech(30000), silence(800), speech(2500)))

	var next int64
	for i, ch := range chs {
		if i > 0 && ch.StartMs < next-1 {
			t.Fatalf("chunk %d starts at %d ms, before the previous one ended (%d ms)", i, ch.StartMs, next)
		}

		next = ch.StartMs + int64(len(ch.Samples))*1000/rate
	}
}

func TestSilenceOnlyIsBoundedAndSilent(t *testing.T) {
	c := New(DefaultConfig())

	var chs []Chunk

	audio := silence(20000)
	for i := 0; i < len(audio); i += 160 {
		chs = append(chs, c.Write(audio[i:i+160])...)
		if len(c.cur) > rate { // more than 1 s buffered
			t.Fatalf("leading silence should be dropped, %d samples buffered", len(c.cur))
		}
	}

	if ch, ok := c.Flush(); ok {
		chs = append(chs, ch)
	}

	if got := len(speechChunks(chs)); got != 0 {
		t.Fatalf("silence produced %d speech chunks", got)
	}
}

func TestTrailingSilence(t *testing.T) {
	c := New(DefaultConfig())
	c.Write(speech(2000))

	if c.TrailingSilenceMs() != 0 {
		t.Fatalf("no trailing silence expected right after speech")
	}

	c.Write(silence(3000))

	if got := c.TrailingSilenceMs(); got < 2800 || got > 3100 {
		t.Fatalf("trailing silence = %d ms, want ~3000", got)
	}
}

func TestNoisyRoom(t *testing.T) {
	// steady background noise well above the absolute floor must not count as speech
	c := New(DefaultConfig())
	room := func(ms int) []float32 { return noise(ms, 0.03) }

	chs := feed(c, concat(room(4000), add(speech(3000), room(3000)), room(1500), add(speech(3000), room(3000)), room(4000)))
	if got := len(speechChunks(chs)); got != 2 {
		t.Fatalf("want 2 speech chunks in a noisy room, got %d", got)
	}
}
