//go:build darwin

package main

import (
	"github.com/brian-nunez/bvoice/internal/ui"
	"github.com/brian-nunez/bvoice/internal/ui/appkit"
)

func newUI() (ui.UI, error) { return appkit.New(), nil }
