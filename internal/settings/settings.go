// Package settings defines bvoice's options in one table that drives defaults, validation,
// command-line flags, the saved-vs-flag diff and the controls the UI renders (PLAN.md §3).
package settings

import (
	"flag"
	"fmt"
	"strconv"
	"strings"
)

// Settings is the effective configuration.
type Settings struct {
	Sounds        bool
	SoundVolume   float64
	Hotkey        string
	ShowWindow    bool
	OnTop         bool
	AutoCopy      bool
	TrailingSpace bool
	Vocabulary    string
	Mic           string // "" = system default
	AutoStopSec   int    // 0 = off
	MaxMinutes    int
	HistoryLimit  int
}

// Defaults returns the out-of-the-box settings.
func Defaults() Settings {
	return Settings{
		Sounds:        true,
		SoundVolume:   0.5,
		Hotkey:        "left_option",
		ShowWindow:    true,
		OnTop:         true,
		AutoCopy:      true,
		TrailingSpace: true,
		MaxMinutes:    30,
		HistoryLimit:  500,
	}
}

// Hotkeys lists the supported modifier-tap hotkeys (PLAN.md §3.2).
var Hotkeys = []string{
	"left_option", "right_option", "left_command", "right_command",
	"left_shift", "right_shift", "left_control", "right_control", "fn",
}

// Kind tells the UI which control to render.
type Kind string

const (
	Bool   Kind = "bool"
	Float  Kind = "float"  // slider between Min and Max
	Int    Kind = "int"    // number field between Min and Max
	Choice Kind = "choice" // pop-up of Choices (Mic choices are filled in at runtime)
	Text   Kind = "text"   // multi-line text
)

// Field describes one option.
type Field struct {
	Key     string // database key; also the flag name
	Label   string // UI label
	Help    string // flag usage / UI tooltip
	Kind    Kind
	Min     float64
	Max     float64
	Choices []string

	get func(*Settings) string
	set func(*Settings, string) error
}

func boolField(key, label, help string, p func(*Settings) *bool) Field {
	return Field{Key: key, Label: label, Help: help, Kind: Bool,
		get: func(s *Settings) string { return strconv.FormatBool(*p(s)) },
		set: func(s *Settings, v string) error {
			b, err := strconv.ParseBool(v)
			if err != nil {
				return fmt.Errorf("%s: want true or false, got %q", key, v)
			}
			*p(s) = b
			return nil
		}}
}

func intField(key, label, help string, min, max int, p func(*Settings) *int) Field {
	return Field{Key: key, Label: label, Help: help, Kind: Int, Min: float64(min), Max: float64(max),
		get: func(s *Settings) string { return strconv.Itoa(*p(s)) },
		set: func(s *Settings, v string) error {
			n, err := strconv.Atoi(strings.TrimSpace(v))
			if err != nil || n < min || n > max {
				return fmt.Errorf("%s: want a whole number from %d to %d, got %q", key, min, max, v)
			}
			*p(s) = n
			return nil
		}}
}

// Fields is the single source of truth for every option, in UI order.
var Fields = []Field{
	boolField("sounds", "Sounds", "play a sound when listening starts and when text is ready",
		func(s *Settings) *bool { return &s.Sounds }),
	{Key: "sound-volume", Label: "Sound volume", Help: "sound volume from 0 to 1", Kind: Float, Min: 0, Max: 1,
		get: func(s *Settings) string { return strconv.FormatFloat(s.SoundVolume, 'f', -1, 64) },
		set: func(s *Settings, v string) error {
			f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err != nil || f < 0 || f > 1 {
				return fmt.Errorf("sound-volume: want a number from 0 to 1, got %q", v)
			}
			s.SoundVolume = f
			return nil
		}},
	{Key: "hotkey", Label: "Hotkey (tap)", Help: "modifier key to tap: " + strings.Join(Hotkeys, ", "),
		Kind: Choice, Choices: Hotkeys,
		get: func(s *Settings) string { return s.Hotkey },
		set: func(s *Settings, v string) error {
			for _, h := range Hotkeys {
				if v == h {
					s.Hotkey = v
					return nil
				}
			}
			return fmt.Errorf("hotkey: want one of %s, got %q", strings.Join(Hotkeys, ", "), v)
		}},
	boolField("window", "Show window on launch", "show the floating window when bvoice starts",
		func(s *Settings) *bool { return &s.ShowWindow }),
	boolField("on-top", "Window always on top", "keep the floating window above other windows",
		func(s *Settings) *bool { return &s.OnTop }),
	boolField("auto-copy", "Copy to clipboard automatically", "copy each transcript to the clipboard when it's ready",
		func(s *Settings) *bool { return &s.AutoCopy }),
	boolField("trailing-space", "Add trailing space", "add a space after each transcript",
		func(s *Settings) *bool { return &s.TrailingSpace }),
	{Key: "vocab", Label: "Custom vocabulary", Help: "names and terms Whisper should expect, e.g. \"Jira, Kubernetes\"",
		Kind: Text,
		get:  func(s *Settings) string { return s.Vocabulary },
		set:  func(s *Settings, v string) error { s.Vocabulary = strings.TrimSpace(v); return nil }},
	{Key: "mic", Label: "Microphone", Help: "microphone name (empty = system default)", Kind: Choice,
		get: func(s *Settings) string { return s.Mic },
		set: func(s *Settings, v string) error { s.Mic = strings.TrimSpace(v); return nil }},
	intField("auto-stop", "Auto-stop after silence (s)", "stop listening after this many seconds of silence (0 = off)",
		0, 3600, func(s *Settings) *int { return &s.AutoStopSec }),
	intField("max-minutes", "Max recording length (min)", "stop listening after this many minutes",
		1, 240, func(s *Settings) *int { return &s.MaxMinutes }),
	intField("history-limit", "History entries to keep", "keep at most this many past transcriptions",
		1, 100000, func(s *Settings) *int { return &s.HistoryLimit }),
}

// FieldByKey returns the field for key.
func FieldByKey(key string) (Field, bool) {
	for _, f := range Fields {
		if f.Key == key {
			return f, true
		}
	}
	return Field{}, false
}

// Get returns the string form of one option.
func (s Settings) Get(key string) (string, error) {
	f, ok := FieldByKey(key)
	if !ok {
		return "", fmt.Errorf("unknown setting %q", key)
	}
	return f.get(&s), nil
}

// Set validates and sets one option.
func (s *Settings) Set(key, value string) error {
	f, ok := FieldByKey(key)
	if !ok {
		return fmt.Errorf("unknown setting %q", key)
	}
	return f.set(s, value)
}

// ToMap serialises every option for storage.
func (s Settings) ToMap() map[string]string {
	m := make(map[string]string, len(Fields))
	for _, f := range Fields {
		m[f.Key] = f.get(&s)
	}
	return m
}

// FromMap loads stored values over the defaults. Unknown keys are ignored and invalid values fall back
// to the default; the returned warnings say which.
func FromMap(m map[string]string) (Settings, []string) {
	s := Defaults()
	var warnings []string
	for _, f := range Fields {
		v, ok := m[f.Key]
		if !ok {
			continue
		}
		if err := f.set(&s, v); err != nil {
			warnings = append(warnings, err.Error()+" (using default)")
		}
	}
	return s, warnings
}

// Overrides holds the options passed as flags for this run.
type Overrides map[string]string

// RegisterFlags adds one flag per option to fs. Call Collect after fs.Parse.
func RegisterFlags(fs *flag.FlagSet) {
	d := Defaults()
	for _, f := range Fields {
		fs.String(f.Key, "", fmt.Sprintf("%s (default %q)", f.Help, f.get(&d)))
	}
	// boolean options also accept a bare --flag meaning true
	for _, f := range Fields {
		if f.Kind == Bool {
			fl := fs.Lookup(f.Key)
			fl.Value = &boolString{v: fl.Value}
		}
	}
}

// boolString lets `--sounds` mean `--sounds=true` while still being stored as a string.
type boolString struct{ v flag.Value }

func (b *boolString) String() string     { return b.v.String() }
func (b *boolString) Set(s string) error { return b.v.Set(s) }
func (b *boolString) IsBoolFlag() bool   { return true }

// Collect returns the options that were explicitly set on the command line, validated.
func Collect(fs *flag.FlagSet) (Overrides, error) {
	o := Overrides{}
	var err error
	fs.Visit(func(fl *flag.Flag) {
		f, ok := FieldByKey(fl.Name)
		if !ok || err != nil {
			return
		}
		v := fl.Value.String()
		probe := Defaults()
		if e := f.set(&probe, v); e != nil {
			err = e
			return
		}
		o[f.Key] = f.get(&probe) // normalised form
	})
	return o, err
}

// Apply returns saved settings with the overrides on top (for this run only — PLAN.md §3.3).
func Apply(saved Settings, o Overrides) Settings {
	s := saved
	for k, v := range o {
		_ = s.Set(k, v) // already validated by Collect
	}
	return s
}

// Change is one option whose launch flag differs from the saved value.
type Change struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Saved string `json:"saved"`
	Flag  string `json:"flag"`
}

// Diff lists the overrides that differ from the saved settings, in field order.
func Diff(saved Settings, o Overrides) []Change {
	var out []Change
	for _, f := range Fields {
		v, ok := o[f.Key]
		if !ok {
			continue
		}
		if sv := f.get(&saved); sv != v {
			out = append(out, Change{Key: f.Key, Label: f.Label, Saved: sv, Flag: v})
		}
	}
	return out
}
