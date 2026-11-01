// Package history holds the transcription entry type shared by the store, controller and UI.
package history

import "time"

// Entry is one saved transcription.
type Entry struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"createdAt"`
	Model     string    `json:"model"`
	AudioMs   int64     `json:"audioMs"`   // recording length
	LatencyMs int64     `json:"latencyMs"` // stop → text ready
	Text      string    `json:"text"`
}
