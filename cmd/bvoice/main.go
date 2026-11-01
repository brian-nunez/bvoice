// Command bvoice is local push-to-talk dictation: tap a modifier key, talk, tap again, and the text
// is on your clipboard. One binary per Whisper model; see docs/architecture.md.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/brian-nunez/bvoice/internal/app"
	"github.com/brian-nunez/bvoice/internal/audio/mic"
	"github.com/brian-nunez/bvoice/internal/model"
	"github.com/brian-nunez/bvoice/internal/platform"
	"github.com/brian-nunez/bvoice/internal/settings"
	"github.com/brian-nunez/bvoice/internal/store"
	"github.com/brian-nunez/bvoice/internal/whisper"
)

// Set by the Makefile via -ldflags.
var (
	version   = "dev"
	modelName = "none"
)

func main() { os.Exit(run(os.Args[1:])) }

func run(args []string) int {
	fs := flag.NewFlagSet("bvoice", flag.ContinueOnError)
	settings.RegisterFlags(fs)
	dataDir := fs.String("data-dir", "", "folder for the settings and history database (default ~/Library/Application Support/bvoice)")
	verbose := fs.Bool("verbose", false, "log details to stderr")
	showVersion := fs.Bool("version", false, "print the version and embedded model, then exit")

	fs.Usage = func() {
		_, _ = fmt.Fprintf(fs.Output(),
			"bvoice %s (model %s) — tap a key, talk, tap again; the text lands on your clipboard.\n\n"+
				"Usage: ./bvoice [flags]\n\n"+
				"Flags change settings for this run only; the window offers to save them.\n\n",
			version, modelName)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}

		return 2
	}

	if fs.NArg() > 0 {
		fmt.Fprintf(os.Stderr, "bvoice: unexpected argument %q (see --help)\n", fs.Arg(0))

		return 2
	}

	if *showVersion {
		fmt.Printf("bvoice %s (model %s)\n", version, modelName)

		return 0
	}

	overrides, err := settings.Collect(fs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bvoice: %v\n", err)

		return 2
	}

	logger := log.New(io.Discard, "", 0)
	if *verbose {
		logger = log.New(os.Stderr, "bvoice: ", log.Ltime|log.Lmicroseconds)
	}

	fail := func(format string, a ...any) int {
		fmt.Fprintf(os.Stderr, "bvoice: "+format+"\n", a...)

		return 1
	}

	dir := *dataDir
	if dir == "" {
		if dir, err = store.DefaultDir(); err != nil {
			return fail("find data folder: %v", err)
		}
	}

	db, err := store.Open(dir)
	if err != nil {
		return fail("open database in %s: %v", dir, err)
	}
	defer func() { _ = db.Close() }()

	stored, err := db.LoadSettings()
	if err != nil {
		return fail("load settings: %v", err)
	}

	saved, warnings := settings.FromMap(stored)
	for _, w := range warnings {
		fmt.Fprintf(os.Stderr, "bvoice: saved setting %s\n", w)
	}

	plat, err := platform.New()
	if err != nil {
		return fail("%v", err)
	}

	ui, err := newUI()
	if err != nil {
		return fail("%v", err)
	}

	recorder, err := mic.NewMic()
	if err != nil {
		return fail("%v", err)
	}
	defer recorder.Close()

	t0 := time.Now()

	m, err := whisper.Load(model.Data, *verbose)
	if err != nil {
		return fail("%v", err)
	}
	defer m.Close()

	logger.Printf("loaded model %s in %v; data in %s", modelName, time.Since(t0).Round(time.Millisecond), dir)

	ctl := app.New(app.Config{
		Model: m, ModelName: modelName, Version: version,
		Recorder: recorder, Store: db, Platform: plat, UI: ui,
		Saved: saved, Overrides: overrides, Log: logger,
	})
	eff := ctl.Settings()
	fmt.Fprintf(os.Stderr, "bvoice %s ready (model %s). Tap %s to start and stop dictating.\n",
		version, modelName, app.HotkeyLabel(eff.Hotkey))

	var startErr error

	err = ui.Run(ctl, eff.ShowWindow, func() {
		if startErr = ctl.Start(); startErr != nil {
			ui.Stop()
		}
	})
	if err != nil {
		return fail("%v", err)
	}

	if startErr != nil {
		return fail("%v", startErr)
	}

	return 0
}
