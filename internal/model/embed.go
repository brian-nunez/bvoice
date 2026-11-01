//go:build bvoice_model

package model

import _ "embed"

// Data is the ggml Whisper model compiled into this binary by `make build MODEL=...`.
//
//go:embed model.bin
var Data []byte
