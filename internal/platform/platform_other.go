//go:build !darwin

package platform

import (
	"errors"
	"runtime"
)

// New is not implemented yet on this OS. A Linux port adds platform_linux.go with its own
// hotkey, clipboard and sound implementations (PLAN.md §10).
func New() (Services, error) {
	return Services{}, errors.New("bvoice does not support " + runtime.GOOS + " yet")
}
