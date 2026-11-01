// Package platform holds the OS services bvoice needs besides audio and UI: the tap hotkey, the
// clipboard and sounds. Each OS provides New in its own file (platform_darwin.go, …).
package platform

// Hotkey watches for a quick tap of a single modifier key.
type Hotkey interface {
	// Watch starts watching key and calls onTap (from a background goroutine) for each tap.
	Watch(key string, onTap func()) error
	// SetKey switches the watched key.
	SetKey(key string) error
	Close()
}

// Clipboard writes text to the system clipboard.
type Clipboard interface {
	SetText(text string) error
}

// Sound identifies a feedback sound.
type Sound int

// Feedback sounds.
const (
	SoundStart Sound = iota // listening started
	SoundDone               // text is ready
	SoundError              // something went wrong
)

// Sounds plays feedback sounds.
type Sounds interface {
	Play(s Sound, volume float64)
}

// Services bundles the platform implementations.
type Services struct {
	Hotkey    Hotkey
	Clipboard Clipboard
	Sounds    Sounds
}
