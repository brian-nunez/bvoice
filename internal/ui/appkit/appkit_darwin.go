//go:build darwin

// Package appkit is the macOS implementation of ui.UI: a menu bar icon (no Dock icon) and a small
// floating, dismissable panel. The Objective-C lives in bridge_darwin.go; this file is the Go side.
// Data crosses the bridge as JSON so the Objective-C never needs to know Go types.
package appkit

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit
#include <stdlib.h>

void bv_ui_run(int show_window);
void bv_ui_stop(void);
void bv_ui_set_json(int what, const char *json);
void bv_ui_set_on_top(int on);
*/
import "C"

import (
	"encoding/json"
	"runtime"
	"strconv"
	"unsafe"

	"github.com/brian-nunez/bvoice/internal/settings"
	"github.com/brian-nunez/bvoice/internal/ui"
)

// AppKit needs the main OS thread; package init runs on it, so pin the main goroutine there.
func init() { runtime.LockOSThread() }

// What each JSON payload is (must match bridge_darwin.go).
const (
	setStatus   = 1
	setHistory  = 2
	setControls = 3
	setNotice   = 4
)

// Action kinds sent from Objective-C (must match bridge_darwin.go).
const (
	actToggle  = 1
	actSetting = 2
	actSearch  = 3
	actCopy    = 4
	actExport  = 5
	actClear   = 6
	actSync    = 7
	actQuit    = 8
)

// UI is the AppKit ui.UI.
type UI struct{}

var (
	actions ui.Actions
	onReady func()
)

// New returns the AppKit UI.
func New() *UI { return &UI{} }

var _ ui.UI = (*UI)(nil)

// Run must be called from the main goroutine. It returns after Stop.
func (u *UI) Run(a ui.Actions, showWindow bool, ready func()) error {
	actions, onReady = a, ready

	show := 0
	if showWindow {
		show = 1
	}

	C.bv_ui_run(C.int(show))

	return nil
}

func (u *UI) send(what int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}

	cs := C.CString(string(b))
	defer C.free(unsafe.Pointer(cs))

	C.bv_ui_set_json(C.int(what), cs)
}

// SetStatus updates the menu bar icon and the window header.
func (u *UI) SetStatus(s ui.Status) { u.send(setStatus, s) }

// SetHistory replaces the rows in the History tab.
func (u *UI) SetHistory(h []ui.HistoryItem) {
	if h == nil {
		h = []ui.HistoryItem{}
	}

	u.send(setHistory, h)
}

// SetControls rebuilds the Settings tab.
func (u *UI) SetControls(c []ui.Control) { u.send(setControls, c) }

// SetSyncNotice shows the sync notice for the given changes, or hides it when there are none.
func (u *UI) SetSyncNotice(ch []settings.Change) {
	if ch == nil {
		ch = []settings.Change{}
	}

	u.send(setNotice, ch)
}

// SetOnTop keeps the window above other windows when on is true.
func (u *UI) SetOnTop(on bool) {
	v := 0
	if on {
		v = 1
	}

	C.bv_ui_set_on_top(C.int(v))
}

// Stop ends the AppKit run loop, which makes Run return.
func (u *UI) Stop() { C.bv_ui_stop() }

//export goUIReady
func goUIReady() {
	if onReady != nil {
		go onReady()
	}
}

// goUIAction is called on the main thread; actions run in a goroutine so the UI never blocks.
//
//export goUIAction
func goUIAction(kind C.int, ckey, cval *C.char) {
	key, val := C.GoString(ckey), C.GoString(cval)

	a := actions
	if a == nil {
		return
	}

	go func() {
		switch int(kind) {
		case actToggle:
			a.ToggleRecording()
		case actSetting:
			a.SetSetting(key, val)
		case actSearch:
			a.Search(val)
		case actCopy:
			if id, err := strconv.ParseInt(val, 10, 64); err == nil {
				a.CopyEntry(id)
			}
		case actExport:
			a.Export(val)
		case actClear:
			a.ClearHistory()
		case actSync:
			a.SyncFlags()
		case actQuit:
			a.Quit()
		}
	}()
}
