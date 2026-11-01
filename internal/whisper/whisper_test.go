package whisper

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readWAV reads a 16-bit PCM mono 16 kHz WAV file into float32 samples.
func readWAV(t *testing.T, path string) []float32 {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) < 44 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		t.Fatalf("%s is not a WAV file", path)
	}
	// walk chunks to find "data"
	for i := 12; i+8 <= len(b); {
		id, size := string(b[i:i+4]), int(binary.LittleEndian.Uint32(b[i+4:i+8]))
		if id == "data" {
			pcm := b[i+8 : min(i+8+size, len(b))]
			out := make([]float32, len(pcm)/2)
			for j := range out {
				out[j] = float32(int16(binary.LittleEndian.Uint16(pcm[2*j:]))) / 32768
			}
			return out
		}
		i += 8 + size + size%2
	}
	t.Fatalf("%s has no data chunk", path)
	return nil
}

func TestLoadRejectsEmptyModel(t *testing.T) {
	if _, err := Load(nil, false); err == nil {
		t.Fatal("expected an error for an empty model")
	}
}

// TestTranscribeSample runs only when BVOICE_TEST_MODEL points at a ggml model file
// (`make test` sets it when build/models has one).
func TestTranscribeSample(t *testing.T) {
	modelPath := os.Getenv("BVOICE_TEST_MODEL")
	if modelPath == "" {
		t.Skip("set BVOICE_TEST_MODEL to a ggml model file to run")
	}
	data, err := os.ReadFile(modelPath)
	if err != nil {
		t.Fatal(err)
	}
	m, err := Load(data, false)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	samples := readWAV(t, filepath.Join("..", "..", "third_party", "whisper.cpp", "samples", "jfk.wav"))
	text, err := m.Transcribe(samples, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(text), "ask not what your country can do for you") {
		t.Fatalf("unexpected transcript: %q", text)
	}
}
