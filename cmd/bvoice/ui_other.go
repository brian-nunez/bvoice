//go:build !darwin

package main

import (
	"errors"
	"runtime"

	"github.com/brian-nunez/bvoice/internal/ui"
)

// Other platforms return their ui.UI implementation here (see docs/architecture.md).
func newUI() (ui.UI, error) {
	return nil, errors.New("bvoice has no UI for " + runtime.GOOS)
}
