# bvoice

Local, private dictation for macOS. Tap **Left Option**, talk, and tap it again: the text is on your
clipboard about a second later, whether you said one sentence or several pages. Everything runs on
your Mac, and each binary has its Whisper model built in.

- Runs as a single executable and needs no Accessibility or Input Monitoring permission. The only
  prompt is macOS's microphone consent.
- A menu bar icon opens a small floating window with status, searchable history (click an entry to
  copy it), export, settings, and a notice when launch flags differ from your saved settings.
- Settings and history are stored in `~/Library/Application Support/bvoice/bvoice.db`.

See [docs/configuration.md](docs/configuration.md) for every setting and flag, and
[docs/architecture.md](docs/architecture.md) for how it works.

## Build

Requirements: Xcode Command Line Tools (`make`, `clang`), Go 1.24 or later, golangci-lint v2 for
`make lint` and `make fmt`, and `gh` for releases.

```sh
make all                          # dist/bvoice-small.en-q5_1 and dist/bvoice-large-v3-turbo-q5_0
make build MODEL=base.en-q5_1     # a binary with any other whisper.cpp model
make model && make test           # tests, including a real transcription with the downloaded model
make lint                         # linters and format checks (.golangci.yml)
make fmt                          # apply gofumpt and goimports
make release TAG=v0.1.0           # build all, tag, push the tag and create a GitHub release
make clean                        # remove build outputs
```

The first build fetches whisper.cpp at a pinned commit and compiles it (Metal shaders embedded),
resolves Go modules, and downloads the model into `build/models/`.

## Run

```sh
./dist/bvoice-small.en-q5_1
./dist/bvoice-large-v3-turbo-q5_0 --sounds=false --auto-stop=10
./dist/bvoice-small.en-q5_1 --help      # all options
```

Flags apply to that run only; the window offers **Sync** to save them. To switch models, quit one
binary and start another. History is shared between them.

| Setting | Default | Flag |
|---|---|---|
| Sounds | on | `--sounds` |
| Sound volume | 0.5 | `--sound-volume` |
| Hotkey | Left Option | `--hotkey` |
| Show window on launch | on | `--window` |
| Window always on top | on | `--on-top` |
| Copy to clipboard automatically | on | `--auto-copy` |
| Add trailing space | on | `--trailing-space` |
| Custom vocabulary | empty | `--vocab` |
| Microphone | system default | `--mic` |
| Auto-stop after silence | off | `--auto-stop` |
| Max recording length | 30 min | `--max-minutes` |
| History entries to keep | 500 | `--history-limit` |

Command-line only: `--data-dir`, `--verbose`, `--version`, `--help`. The
[configuration reference](docs/configuration.md) covers allowed values, behavior and examples for
each.

## Manual test checklist

These need a real Mac:

1. **Permissions:** the only prompt is the microphone one, on the first dictation. Tapping the hotkey
   works without Accessibility or Input Monitoring.
2. **Tap detection:** ⌥-shortcuts (⌥←, ⌥-click, typing ⌥e) don't start dictation; a quick tap does.
3. **Short and long dictation:** text is ready about a second after stopping. `--verbose` logs
   per-chunk timing and the latency after stop.
4. **Clipboard and sounds:** ⌘V pastes the text with a trailing space; sound and volume settings apply.
5. **Window:** the menu bar icon shows and hides it, `Esc` and the close button hide it, right-click
   opens the menu, "always on top" toggles, and it never takes focus from the app you're typing in.
6. **History:** search, click to copy, **Export…** writes Markdown, **Clear History…** asks first.
7. **Flags:** starting with a value that differs from the saved one shows the sync notice; **Sync**
   saves it and hides the notice.
8. **Models:** both default binaries build and run; History shows each one's "ready in" time.

## Layout

```
cmd/bvoice           entry point and flags
internal/app         controller (state machine), platform-independent and tested with fakes
internal/vad         pause detection that drives background transcription
internal/whisper     whisper.cpp via cgo; model loaded from memory
internal/settings    option table: defaults, flags, validation, sync diff, UI controls
internal/store       SQLite settings and history
internal/platform    hotkey, clipboard and sounds (macOS in platform_darwin.go)
internal/ui          UI contract; ui/appkit is the macOS implementation
docs                 configuration reference and architecture
```
