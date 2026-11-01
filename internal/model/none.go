//go:build !bvoice_model

// Package model holds the Whisper model embedded at build time.
// Plain `go build`/`go test` (without the bvoice_model tag) compile with no model so the rest of the
// code can be built and tested; real binaries come from `make build`.
package model

// Data is empty when built without the bvoice_model tag.
var Data []byte
