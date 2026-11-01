//go:build darwin

package platform

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework AppKit -framework CoreGraphics

#import <AppKit/AppKit.h>
#import <CoreGraphics/CoreGraphics.h>
#include <stdlib.h>

// Reading the modifier state of the HID system needs no Accessibility or Input Monitoring
// permission (unlike event taps). Verify this on each new macOS version (PLAN.md §1).
static uint64_t bv_modifier_flags(void) {
	return (uint64_t)CGEventSourceFlagsState(kCGEventSourceStateHIDSystemState);
}

// Count of key-down and mouse-down events so far; if it changes during a modifier press,
// the press was part of a shortcut or a modifier-click, not a tap.
static uint64_t bv_input_count(void) {
	CGEventSourceStateID s = kCGEventSourceStateHIDSystemState;
	return (uint64_t)CGEventSourceCounterForEventType(s, kCGEventKeyDown)
	     + (uint64_t)CGEventSourceCounterForEventType(s, kCGEventLeftMouseDown)
	     + (uint64_t)CGEventSourceCounterForEventType(s, kCGEventRightMouseDown)
	     + (uint64_t)CGEventSourceCounterForEventType(s, kCGEventOtherMouseDown);
}

static void bv_on_main(dispatch_block_t b) {
	if ([NSThread isMainThread]) b(); else dispatch_sync(dispatch_get_main_queue(), b);
}

static int bv_set_clipboard(const char *utf8) {
	__block BOOL ok = NO;
	NSString *s = [NSString stringWithUTF8String:utf8];
	bv_on_main(^{
		NSPasteboard *pb = [NSPasteboard generalPasteboard];
		[pb clearContents];
		ok = [pb setString:s forType:NSPasteboardTypeString];
	});
	return ok ? 0 : 1;
}

static void bv_play_sound(const char *name, double volume) {
	NSString *n = [NSString stringWithUTF8String:name];
	dispatch_async(dispatch_get_main_queue(), ^{
		NSSound *snd = [NSSound soundNamed:n];
		if (!snd) return;
		[snd stop];
		[snd setVolume:(float)volume];
		[snd play];
	});
}
*/
import "C"

import (
	"fmt"
	"sync"
	"time"
	"unsafe"
)

// New returns the macOS services.
func New() (Services, error) {
	return Services{Hotkey: &tapHotkey{}, Clipboard: pasteboard{}, Sounds: systemSounds{}}, nil
}

// ---------------------------------------------------------------------------------- hotkey

// Device-dependent modifier bits (IOKit NX_DEVICE*KEYMASK) and generic class masks.
const (
	devLCtrl  = 0x00000001
	devLShift = 0x00000002
	devRShift = 0x00000004
	devLCmd   = 0x00000008
	devRCmd   = 0x00000010
	devLAlt   = 0x00000020
	devRAlt   = 0x00000040
	devRCtrl  = 0x00002000

	maskShift = 0x00020000
	maskCtrl  = 0x00040000
	maskAlt   = 0x00080000
	maskCmd   = 0x00100000
	maskFn    = 0x00800000

	allModifiers = maskShift | maskCtrl | maskAlt | maskCmd | maskFn |
		devLCtrl | devLShift | devRShift | devLCmd | devRCmd | devLAlt | devRAlt | devRCtrl
)

// keyMasks maps a hotkey name to (its own bit, the bits that are allowed while it's down).
var keyMasks = map[string][2]uint64{
	"left_option":   {devLAlt, devLAlt | maskAlt},
	"right_option":  {devRAlt, devRAlt | maskAlt},
	"left_command":  {devLCmd, devLCmd | maskCmd},
	"right_command": {devRCmd, devRCmd | maskCmd},
	"left_shift":    {devLShift, devLShift | maskShift},
	"right_shift":   {devRShift, devRShift | maskShift},
	"left_control":  {devLCtrl, devLCtrl | maskCtrl},
	"right_control": {devRCtrl, devRCtrl | maskCtrl},
	"fn":            {maskFn, maskFn},
}

const (
	pollEvery = 10 * time.Millisecond
	maxTap    = 400 * time.Millisecond
)

type tapHotkey struct {
	mu    sync.Mutex
	key   string
	stop  chan struct{}
	onTap func()
}

func (h *tapHotkey) Watch(key string, onTap func()) error {
	if _, ok := keyMasks[key]; !ok {
		return fmt.Errorf("unsupported hotkey %q", key)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stop != nil {
		close(h.stop)
	}
	h.key, h.onTap, h.stop = key, onTap, make(chan struct{})
	go h.poll(h.stop)
	return nil
}

func (h *tapHotkey) SetKey(key string) error {
	if _, ok := keyMasks[key]; !ok {
		return fmt.Errorf("unsupported hotkey %q", key)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.key = key
	return nil
}

func (h *tapHotkey) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stop != nil {
		close(h.stop)
		h.stop = nil
	}
}

// poll implements the tap rule from PLAN.md §2: the key goes down and up within maxTap, with no
// other modifier held and no key or mouse press in between.
func (h *tapHotkey) poll(stop chan struct{}) {
	t := time.NewTicker(pollEvery)
	defer t.Stop()
	var (
		wasDown  bool
		downAt   time.Time
		dirty    bool
		countAt  uint64
		watching string
	)
	for {
		select {
		case <-stop:
			return
		case now := <-t.C:
			h.mu.Lock()
			key, onTap := h.key, h.onTap
			h.mu.Unlock()
			if key != watching { // hotkey changed: reset
				watching, wasDown = key, false
			}
			m := keyMasks[key]
			flags := uint64(C.bv_modifier_flags())
			down := flags&m[0] != 0
			others := flags & allModifiers &^ m[1]
			switch {
			case down && !wasDown:
				downAt, dirty, countAt = now, others != 0, uint64(C.bv_input_count())
			case down && others != 0:
				dirty = true
			case !down && wasDown:
				if !dirty && now.Sub(downAt) <= maxTap && uint64(C.bv_input_count()) == countAt {
					go onTap()
				}
			}
			wasDown = down
		}
	}
}

// ---------------------------------------------------------------------------------- clipboard

type pasteboard struct{}

func (pasteboard) SetText(text string) error {
	cs := C.CString(text)
	defer C.free(unsafe.Pointer(cs))
	if C.bv_set_clipboard(cs) != 0 {
		return fmt.Errorf("the clipboard refused the text")
	}
	return nil
}

// ---------------------------------------------------------------------------------- sounds

type systemSounds struct{}

var soundNames = map[Sound]string{SoundStart: "Tink", SoundDone: "Glass", SoundError: "Basso"}

func (systemSounds) Play(s Sound, volume float64) {
	name, ok := soundNames[s]
	if !ok {
		return
	}
	cs := C.CString(name)
	defer C.free(unsafe.Pointer(cs))
	C.bv_play_sound(cs, C.double(volume))
}
