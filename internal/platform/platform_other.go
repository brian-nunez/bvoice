//go:build !darwin

package platform

import (
	"errors"
	"runtime"
)

// New is not implemented on this OS. A port adds platform_<os>.go with its own hotkey,
// clipboard and sound implementations.
func New() (Services, error) {
	return Services{}, errors.New("bvoice does not support " + runtime.GOOS)
}
