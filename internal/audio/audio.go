// Package audio defines microphone capture for bvoice. Implementations live in subpackages
// (mic: miniaudio/CoreAudio) so the controller can be tested with a fake.
package audio

// SampleRate is what Whisper expects (16 kHz mono float32).
const SampleRate = 16000

// Device is a capture device.
type Device struct {
	Name    string `json:"name"`
	Default bool   `json:"default"`
}

// Recorder captures audio.
type Recorder interface {
	Devices() ([]Device, error)
	// Start opens the named device ("" = system default) and calls onSamples from the audio thread.
	// onSamples must return quickly and must not keep the slice.
	Start(device string, onSamples func([]float32)) error
	// Stop closes the device. After it returns, onSamples is no longer called.
	Stop() error
}
