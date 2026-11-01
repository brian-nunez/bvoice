// Package whisper is a small cgo wrapper over whisper.cpp, statically linked by the Makefile.
package whisper

/*
#cgo CFLAGS: -I${SRCDIR}/../../third_party/whisper.cpp/include -I${SRCDIR}/../../third_party/whisper.cpp/ggml/include
#cgo LDFLAGS: -L${SRCDIR}/../../build/whisper -lwhisper
#cgo darwin LDFLAGS: -lc++ -framework Accelerate -framework Metal -framework MetalKit -framework Foundation
#cgo linux LDFLAGS: -lstdc++ -lm -ldl -lpthread
#include <stdlib.h>
#include <whisper.h>

static void bv_quiet_log(enum ggml_log_level level, const char *text, void *user) {}
static void bv_set_logging(int verbose) { if (!verbose) whisper_log_set(bv_quiet_log, NULL); }
*/
import "C"

import (
	"errors"
	"runtime"
	"strings"
	"sync"
	"unsafe"
)

// Model is a loaded Whisper model. It is safe for concurrent use; calls are serialized.
type Model struct {
	mu      sync.Mutex
	ctx     *C.struct_whisper_context
	threads int
}

// Load initialises whisper.cpp from an in-memory ggml model (the one embedded in the binary).
func Load(data []byte, verbose bool) (*Model, error) {
	if len(data) == 0 {
		return nil, errors.New("no Whisper model is embedded in this binary; build it with `make build MODEL=...`")
	}
	if verbose {
		C.bv_set_logging(1)
	} else {
		C.bv_set_logging(0)
	}
	params := C.whisper_context_default_params()
	ctx := C.whisper_init_from_buffer_with_params(unsafe.Pointer(&data[0]), C.size_t(len(data)), params)
	if ctx == nil {
		return nil, errors.New("whisper.cpp could not load the embedded model")
	}
	threads := runtime.NumCPU()
	if threads > 8 {
		threads = 8
	}
	return &Model{ctx: ctx, threads: threads}, nil
}

// Transcribe converts 16 kHz mono float32 samples to text. prompt (may be empty) primes the decoder
// with vocabulary and the preceding text. The returned text is raw Whisper output.
func (m *Model) Transcribe(samples []float32, prompt string) (string, error) {
	if len(samples) == 0 {
		return "", nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx == nil {
		return "", errors.New("model is closed")
	}

	p := C.whisper_full_default_params(C.WHISPER_SAMPLING_GREEDY)
	lang := C.CString("en")
	defer C.free(unsafe.Pointer(lang))
	p.language = lang
	p.n_threads = C.int(m.threads)
	p.no_context = C.bool(true)
	p.no_timestamps = C.bool(true)
	p.suppress_blank = C.bool(true)
	p.suppress_nst = C.bool(true)
	p.print_special = C.bool(false)
	p.print_progress = C.bool(false)
	p.print_realtime = C.bool(false)
	p.print_timestamps = C.bool(false)
	if prompt = strings.TrimSpace(prompt); prompt != "" {
		cPrompt := C.CString(prompt)
		defer C.free(unsafe.Pointer(cPrompt))
		p.initial_prompt = cPrompt
	}

	if C.whisper_full(m.ctx, p, (*C.float)(unsafe.Pointer(&samples[0])), C.int(len(samples))) != 0 {
		return "", errors.New("whisper.cpp transcription failed")
	}
	var b strings.Builder
	n := int(C.whisper_full_n_segments(m.ctx))
	for i := 0; i < n; i++ {
		b.WriteString(C.GoString(C.whisper_full_get_segment_text(m.ctx, C.int(i))))
	}
	return b.String(), nil
}

// Close frees the model.
func (m *Model) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx != nil {
		C.whisper_free(m.ctx)
		m.ctx = nil
	}
}
