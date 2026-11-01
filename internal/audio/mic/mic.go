// Package mic is the miniaudio-backed audio.Recorder (CoreAudio on macOS, ALSA/Pulse on Linux).
package mic

import (
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/gen2brain/malgo"

	"github.com/brian-nunez/bvoice/internal/audio"
)

var _ audio.Recorder = (*Mic)(nil)

// Mic is the miniaudio-backed recorder (CoreAudio on macOS). The device is opened only while
// recording, so the macOS mic indicator is only on while bvoice is listening.
type Mic struct {
	mu  sync.Mutex
	ctx *malgo.AllocatedContext
	dev *malgo.Device
}

// NewMic initialises the audio context.
func NewMic() (*Mic, error) {
	ctx, err := malgo.InitContext(nil, malgo.ContextConfig{}, func(string) {})
	if err != nil {
		return nil, fmt.Errorf("init audio: %w", err)
	}

	return &Mic{ctx: ctx}, nil
}

// Devices lists capture devices.
func (m *Mic) Devices() ([]audio.Device, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	infos, err := m.ctx.Devices(malgo.Capture)
	if err != nil {
		return nil, err
	}

	out := make([]audio.Device, 0, len(infos))
	for _, in := range infos {
		out = append(out, audio.Device{Name: in.Name(), Default: in.IsDefault != 0})
	}

	return out, nil
}

// Start begins capturing.
func (m *Mic) Start(device string, onSamples func([]float32)) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.dev != nil {
		return errors.New("already recording")
	}

	cfg := malgo.DefaultDeviceConfig(malgo.Capture)
	cfg.Capture.Format = malgo.FormatF32
	cfg.Capture.Channels = 1
	cfg.SampleRate = audio.SampleRate

	if device != "" {
		infos, err := m.ctx.Devices(malgo.Capture)
		if err != nil {
			return err
		}

		found := false

		for i := range infos {
			if infos[i].Name() == device {
				cfg.Capture.DeviceID = infos[i].ID.Pointer()
				found = true

				break
			}
		}

		if !found {
			return fmt.Errorf("microphone %q not found", device)
		}
	}

	buf := make([]float32, 0, 4096)

	dev, err := malgo.InitDevice(m.ctx.Context, cfg, malgo.DeviceCallbacks{
		Data: func(_, in []byte, frames uint32) {
			n := int(frames)
			if n == 0 || len(in) < n*4 {
				return
			}

			src := unsafe.Slice((*float32)(unsafe.Pointer(&in[0])), n)
			buf = append(buf[:0], src...)
			onSamples(buf)
		},
	})
	if err != nil {
		return fmt.Errorf("open microphone: %w", err)
	}

	if err := dev.Start(); err != nil {
		dev.Uninit()

		return fmt.Errorf("start microphone: %w", err)
	}

	m.dev = dev

	return nil
}

// Stop ends capturing. After it returns, onSamples is no longer called.
func (m *Mic) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.dev == nil {
		return nil
	}

	err := m.dev.Stop()
	m.dev.Uninit()
	m.dev = nil

	return err
}

// Close releases the audio context.
func (m *Mic) Close() {
	_ = m.Stop()
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ctx != nil {
		_ = m.ctx.Uninit()
		m.ctx.Free()
		m.ctx = nil
	}
}
