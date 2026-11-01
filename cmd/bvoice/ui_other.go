//go:build !darwin

package main

import (
	"errors"
	"runtime"

	"github.com/brian-nunez/bvoice/internal/ui"
)

// A Linux UI goes in its own package and is returned here (PLAN.md §6).
func newUI() (ui.UI, error) {
	return nil, errors.New("bvoice has no UI for " + runtime.GOOS + " yet")
}
