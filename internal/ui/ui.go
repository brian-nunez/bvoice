// Package ui is the contract between the controller and a UI toolkit.
// The controller pushes view models in; the UI sends user actions back through Actions.
// Each toolkit (ui/appkit on macOS) implements UI in its own package.
package ui

import "github.com/brian-nunez/bvoice/internal/settings"

// Phase is the dictation state.
type Phase string

// Dictation phases.
const (
	Idle      Phase = "idle"
	Listening Phase = "listening"
	Finishing Phase = "finishing"
)

// Status is shown in the menu bar icon and the window header.
type Status struct {
	Phase       Phase  `json:"phase"`
	StartedAtMs int64  `json:"startedAtMs"` // unix ms when listening began (for the elapsed timer)
	Model       string `json:"model"`
	Version     string `json:"version"`
	Hotkey      string `json:"hotkey"`
	Message     string `json:"message"` // last info or error, "" for none
}

// HistoryItem is one row in the history list.
type HistoryItem struct {
	ID      int64  `json:"id"`
	Time    string `json:"time"`    // e.g. "Jan 2 15:04"
	Meta    string `json:"meta"`    // e.g. "small.en-q5_1 · 0:42 · ready in 0.9s"
	Preview string `json:"preview"` // first line(s) of the text
}

// Control is one setting rendered in the window.
type Control struct {
	Key     string        `json:"key"`
	Label   string        `json:"label"`
	Help    string        `json:"help"`
	Kind    settings.Kind `json:"kind"`
	Value   string        `json:"value"`
	Min     float64       `json:"min"`
	Max     float64       `json:"max"`
	Choices []Choice      `json:"choices,omitempty"`
}

// Choice is one option of a Choice control.
type Choice struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Actions are what the user can do in the UI. Implementations must not block the UI thread for
// long; the controller's methods return quickly and do slow work in the background.
type Actions interface {
	ToggleRecording()
	SetSetting(key, value string)
	Search(query string)
	CopyEntry(id int64)
	Export(path string)
	ClearHistory()
	SyncFlags()
	Quit()
}

// UI is implemented by a toolkit. All methods except Run may be called from any goroutine.
type UI interface {
	// Run takes over the calling (main) thread, shows the UI and returns after Stop.
	// ready is called once the UI can accept updates.
	Run(a Actions, showWindow bool, ready func()) error
	SetStatus(Status)
	SetHistory([]HistoryItem)
	SetControls([]Control)
	// SetSyncNotice shows the "started with different options" notice; nil or empty hides it.
	SetSyncNotice([]settings.Change)
	SetOnTop(bool)
	Stop()
}
