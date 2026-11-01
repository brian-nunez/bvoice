# bvoice

Local, private dictation for macOS. Tap **Left Option**, talk, tap it again — the text is on your
clipboard about a second later, whether you said one sentence or several pages. Everything runs on
your Mac; each binary has its Whisper model built in. See [PLAN.md](PLAN.md) for the full spec.

- No install, no Accessibility or Input Monitoring permission. The only prompt is macOS's one-time
  microphone consent (none at all if your terminal already has mic access).
- Menu bar icon + a small floating window: status, searchable history (click to copy), export,
  settings, and a notice when launch flags differ from your saved settings.
- Settings and history live in `~/Library/Application Support/bvoice/bvoice.db`.

## Build (on your Mac)

Prerequisites: Xcode Command Line Tools (for `make` and `clang`), Go ≥ 1.24, and `gh` for releases. No CMake.

```sh
make all                          # dist/bvoice-small.en-q5_1 and dist/bvoice-large-v3-turbo-q5_0
make build MODEL=base.en-q5_1     # any other whisper.cpp model, to compare
make model && make test           # tests, including a real transcription with the downloaded model
make release TAG=v0.1.0           # build all, tag, push the tag, create the GitHub release
```

The first `make` fetches whisper.cpp (pinned) and compiles it with plain compiler rules (Metal
shaders embedded), resolves Go modules (creating `go.sum` — commit it), and downloads the model from
Hugging Face into `build/models/`.

## Run

```sh
./dist/bvoice-small.en-q5_1
./dist/bvoice-large-v3-turbo-q5_0 --sounds=false --auto-stop=10
./dist/bvoice-small.en-q5_1 --help      # every option
```

Flags apply to that run only; the window offers **Sync** to save them. Switch models by quitting one
binary and starting another — history is shared between them.

## First-run checklist (M3)

Things that could only be verified on the Mac:

1. **No permissions:** start it; the only prompt should be the microphone one, on first dictation.
   Tapping Left Option must work without granting Accessibility or Input Monitoring.
2. **Tap detection:** ⌥-shortcuts (⌥←, ⌥-click, typing ⌥e) must not start dictation; a quick tap must.
3. **Short and long:** one sentence, then a few minutes; text should be ready ~1 s after stopping.
   `--verbose` logs per-chunk timing and the stop → ready latency.
4. **Clipboard + sounds:** ⌘V pastes the text (with a trailing space); toggle sounds and volume.
5. **Window:** menu bar icon opens/closes it, Esc and the close button hide it, right-click the icon
   for a menu, "always on top" toggles, the window never steals focus from the app you're typing in.
6. **History:** search, click to copy, Export… writes Markdown, Clear History… asks first.
7. **Flags:** `--sounds=false` with sounds saved on shows the sync notice; **Sync** saves and hides it.
8. **Both models** build and run; compare their "ready in" times in History.

## Layout

```
cmd/bvoice           entry point, flags
internal/app         controller (state machine) — platform-independent, tested with fakes
internal/vad         pause detection that drives background transcription
internal/whisper     whisper.cpp via cgo; model loaded from memory
internal/settings    option table: defaults, flags, validation, sync diff, UI controls
internal/store       SQLite settings + history
internal/platform    hotkey, clipboard, sounds (macOS in platform_darwin.go)
internal/ui          UI contract; ui/appkit is the macOS implementation
```

Linux later = `platform_linux.go`, a UI package implementing `ui.UI`, and a case in `cmd/bvoice/ui_*.go`.
