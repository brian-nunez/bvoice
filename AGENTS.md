# AGENTS.md

Guidance for coding agents working in this repository. Read this before making changes.

## Project summary

bvoice is a local speech-to-text dictation tool for macOS, written in Go with cgo. Tapping a modifier
key starts listening; tapping again stops it, and the transcript is copied to the clipboard. Each
release binary embeds one Whisper model and statically links whisper.cpp, SQLite and miniaudio.

- [README.md](README.md): usage, build commands and the manual test checklist
- [docs/configuration.md](docs/configuration.md): every setting and flag
- [docs/architecture.md](docs/architecture.md): design, pipeline, code layout and data schema

## Invariants

These are product requirements. Do not break them, and stop and ask before working around one.

1. **One self-contained binary.** Everything ships inside the executable: the model, whisper.cpp,
   Metal shaders, SQLite and miniaudio. Nothing may need to sit beside it.
2. **No special permissions.** bvoice must run without Accessibility, Input Monitoring, Full Disk
   Access, Login Items or any other System Settings change. The only allowed prompt is macOS's
   microphone consent. This rules out:
   - event taps, `CGEventPost` and any synthetic keystrokes (so no auto-paste);
   - global hotkeys other than modifier-key taps detected by polling `CGEventSourceFlagsState`;
   - launch-at-login and login items;
   - reading or writing outside the data folder (default `~/Library/Application Support/bvoice`).
3. **Fully local.** No network access at runtime. The only network use is at build time: fetching
   whisper.cpp, Go modules and models.
4. **Platform-independent core.** `internal/app`, `vad`, `settings`, `textproc`, `history`, `audio`
   (interface) and `ui` (contract) must not import OS-specific code. They have to build and test on
   any OS.
5. **English only.** Transcription runs with `language="en"`.

## Commands

| Task | Command |
|---|---|
| Build the default binaries | `make all` (writes `dist/bvoice-small.en-q5_1` and `dist/bvoice-large-v3-turbo-q5_0`) |
| Build with one model | `make build MODEL=<whisper.cpp model name>` |
| Build only whisper.cpp | `make lib` (writes `build/whisper/libwhisper.a`) |
| Download a model | `make model MODEL=<name>` (writes `build/models/ggml-<name>.bin`) |
| Run all tests | `make test` |
| Vet | `make vet` |
| Lint | `make lint` (golangci-lint v2, configured in `.golangci.yml`) |
| Format | `make fmt` (gofumpt and goimports) |
| Test the release scripts | `make test-scripts` |
| Build release files | `make dist VERSION=vX.Y.Z` (cleans `dist/`, builds all, writes `dist/SHA256SUMS`) |
| Release | Automatic on push to `main` (see [Release](#release)) |
| Clean build outputs | `make clean` (keeps downloaded models and the whisper.cpp checkout) |

`make test` runs `go test ./...` with `BVOICE_TEST_MODEL` set to the first file in `build/models/`.
That enables a real transcription test of whisper.cpp's sample clip. Run `make model` first to include
it; without a model, that test is skipped.

### Before finishing a change

1. `make fmt` leaves no changes.
2. `make lint` reports 0 issues, and `make vet` passes.
3. `go test -race ./...` passes, and `make test-scripts` passes if `scripts/` changed.
4. Docs are updated if behavior, flags, settings, build steps or layout changed (see below).

## Code layout

```
cmd/bvoice/            entry point: flag parsing, wiring, per-OS UI selection (ui_darwin.go / ui_other.go)
internal/app/          controller: dictation state machine and glue; talks only to interfaces
internal/vad/          pause detection and chunking for background transcription (pure Go)
internal/whisper/      cgo wrapper over whisper.cpp; loads the embedded model from memory
internal/model/        embedded model: embed.go (tag bvoice_model) and none.go (no model)
internal/settings/     the option table: defaults, validation, flags, sync diff, UI control metadata
internal/store/        SQLite settings and history, with migrations
internal/history/      history Entry type
internal/textproc/     transcript cleanup and Whisper prompt building
internal/audio/        Recorder interface; audio/mic is the miniaudio implementation
internal/platform/     Hotkey/Clipboard/Sounds interfaces; platform_darwin.go and platform_other.go
internal/ui/           UI contract and view models; ui/appkit is the AppKit implementation
docs/                  configuration reference and architecture
scripts/               release helpers: next-version.sh and its tests
.github/workflows/     ci.yml (checks on push and pull request) and release.yml (automatic releases)
```

Generated or downloaded paths are gitignored and must never be committed: `build/`, `dist/`,
`third_party/` and `internal/model/model.bin`.

## How things fit together

- `cmd/bvoice/main.go` parses flags, opens the store, loads saved settings, builds the platform
  services, recorder, Whisper model and UI, and hands them to `app.New`. `UI.Run` takes over the main
  thread; `Controller.Start` runs once the UI is ready.
- `app.Controller` implements `ui.Actions`. It pushes view models to the UI (`SetStatus`,
  `SetHistory`, `SetControls`, `SetSyncNotice`, `SetOnTop`) and never blocks the UI thread: slow work
  runs in goroutines.
- During dictation the audio callback feeds `vad.Chunker`. Completed speech chunks go through a
  buffered channel to a single worker goroutine, which transcribes them in order. On stop, the final
  partial chunk is flushed, the worker drains, and `textproc.Join` builds the transcript.
- The model is embedded with `//go:embed` only when building with `-tags bvoice_model` (the Makefile
  does this). A plain `go build` or `go test` compiles with an empty model, and `whisper.Load` returns a
  clear error.

## Recipes

### Add or change a setting

All settings are defined in one table, `settings.Fields` in `internal/settings/settings.go`. Adding an
entry automatically provides the flag, validation, database storage, the sync diff and the window
control.

1. Add the field to the `Settings` struct and its default to `Defaults()`.
2. Add an entry to `Fields`, using `boolField`, `intField` or a custom `Field` with `get`/`set`. The
   `Key` becomes both the flag name and the database key, so keep it lowercase with hyphens. The
   entry's position in `Fields` sets its order in the window.
3. Use the setting where it matters, usually in `internal/app`. If a change must take effect
   immediately, handle the key in `Controller.SetSetting`.
4. Add tests in `internal/settings/settings_test.go` and, for behavior, `internal/app/app_test.go`.
   `TestEveryFieldHasAFlagAndDefault` catches invalid defaults.
5. Document it in `docs/configuration.md` (summary table and its own section) and the README's
   settings table.

Never rename an existing key without a migration: saved databases use it.

### Add a command-line-only flag

Register it in `cmd/bvoice/main.go` with the standard `flag` package. Don't add it to
`settings.Fields`, which is only for settings that are saved and shown in the window. Document it in
`docs/configuration.md` under "Command-line-only flags".

### Change the database schema

Append a new SQL string to `migrations` in `internal/store/store.go`. Never edit an existing
migration, since installed databases have already applied it. Migrations run in order inside a
transaction, and `schema_version` records progress. Update the schema in `docs/architecture.md`.

### Change the UI

- The UI only renders view models from `internal/ui/ui.go` and reports actions through `ui.Actions`.
  Keep logic in `internal/app`, not in the UI.
- To add something to the window, extend the view models or `Actions` in `internal/ui/ui.go` first,
  then implement it in `internal/ui/appkit` and the test fake in `internal/app/app_test.go`.
- `ui/appkit/appkit_darwin.go` is the Go side; `ui/appkit/bridge_darwin.go` holds the Objective-C.
  Data crosses as JSON (`bv_ui_set_json`), and actions come back through `goUIAction` with the integer
  kinds defined in both files. Keep the two enums in sync.
- All AppKit calls must happen on the main thread. The bridge dispatches to the main queue; follow
  that pattern.
- The panel must stay non-activating, so it never takes focus from the app being dictated into.

### cgo rules

- Objective-C files use `#cgo CFLAGS: -x objective-c -fobjc-arc`.
- Free every `C.CString` with `C.free`.
- `internal/whisper/whisper.go` links `build/whisper/libwhisper.a` through `${SRCDIR}`-relative
  paths. If library paths change in the Makefile, update its `#cgo LDFLAGS` too.

### Update whisper.cpp

The Makefile lists whisper.cpp's source files and compile definitions explicitly, so a new commit can
change what it needs.

1. Update `WHISPER_COMMIT`, and set `WHISPER_VERSION` and `GGML_VERSION` to the versions declared by
   that commit.
2. Compare the Makefile's source lists (`BASE_SRC`, `REG_SRC`, `CPU_SRC`, `WHI_SRC`, `METAL_SRC`,
   `BLAS_SRC`) and defines with the build definitions in the new checkout's `src/`, `ggml/src/`,
   `ggml/src/ggml-cpu/`, `ggml/src/ggml-metal/` and `ggml/src/ggml-blas/` folders. Add new source
   files and remove deleted ones.
3. Check the Metal kernel list. The Makefile embeds every `kernels/*.metal` file except those that are
   only included by others (currently `fa_common` and `fa_vec_common`). The embedded names must match
   the `X(...)` list in `ggml/src/ggml-metal/ggml-metal-device.m`.
4. `rm -rf third_party/whisper.cpp build/whisper && make lib`, then `make test` with a model, and
   build and run both default binaries on a Mac.

### Change the hotkey or tap detection

Tap detection is in `internal/platform/platform_darwin.go` (`tapHotkey.poll`). It must stay
permission-free: poll `CGEventSourceFlagsState` and `CGEventSourceCounterForEventType` only. A tap
means the key goes down and up within `maxTap` (400 ms), no other modifier is held, and no key or
mouse press occurs in between. New hotkey names go in `settings.Hotkeys` and `keyMasks`.

### Release

Releases are made only by GitHub Actions; never tag or release by hand.

1. A push to `main` runs `.github/workflows/ci.yml`: `make fmt` with no changes, `make vet`,
   `make lint`, `go test -race ./...` with the `small.en-q5_1` model, and `make test-scripts`.
2. When CI passes, `.github/workflows/release.yml` runs `scripts/next-version.sh`, which reads the
   Conventional Commits since the latest `vX.Y.Z` tag: a breaking change bumps major, `feat` minor,
   anything else patch, and only `docs`, `ci`, `chore`, `test` or `style` commits means no release.
   The first release is `v0.1.0`.
3. The workflow runs `make dist VERSION=<version> CPU_ARCH=-mcpu=apple-m1`, verifies the checksums,
   each binary's ad-hoc signature and its `--version` output, then `gh release create` makes the tag
   at the tested commit and a release with generated notes, both binaries and `SHA256SUMS`.

Commit types therefore decide version numbers, so pick them carefully. Both workflows build on
`macos-15` (Apple silicon) and can be rerun with `workflow_dispatch`. whisper.cpp and the models are
cached between runs, keyed on the `Makefile` and the model name. CI and the release must use the same
`CPU_ARCH`: whisper.cpp otherwise defaults to `-mcpu=native`, which ties the binary to the runner's
CPU.

Binaries are large (about 200 MB and 560 MB); they belong in releases, never in the repository.

## Conventions

### Go

- Follow `.golangci.yml`: gofumpt and goimports formatting (imports from this module grouped last),
  and the enabled linters, including `wsl_v5` and `nlreturn` blank-line rules, `revive` doc comments
  on exported identifiers, and `errcheck`. Fix findings rather than adding `//nolint`.
- Errors that can't be handled are discarded explicitly (`_ = f.Close()`); errors from cleanup after
  a failure are combined with `errors.Join`.
- Wrap errors with context (`fmt.Errorf("open database: %w", err)`). Errors shown to users should
  say what failed in plain words.
- The controller never logs to stdout. Logging goes to the injected `*log.Logger`, which writes to
  stderr only with `--verbose`.
- Concurrency: the controller's state is guarded by `Controller.mu`, and session audio state by
  `session.mu`. Don't call UI or platform methods while holding `Controller.mu`.
- Tests use the fakes in `internal/app/app_test.go`. Extend them rather than adding real
  dependencies to controller tests.

### Comments and documentation

- Comments and docs describe the code and product as they are. Do not reference planning
  discussions, chat context, who decided something, or future plans and roadmaps.
- Don't cite document section numbers from code. Point to a file (`docs/architecture.md`) if needed.
- Keep `README.md`, `docs/configuration.md` and `docs/architecture.md` in sync with behavior in the
  same change.

### Commits and pull requests

- Use [Conventional Commits](https://www.conventionalcommits.org/): `type(scope): summary`, with
  types such as `feat`, `fix`, `docs`, `refactor`, `test`, `build`, `chore` and `perf`. Use the
  imperative mood, lowercase, and no trailing period, e.g. `fix(vad): keep lead-in when dropping
  silence`.
- Commits are authored by the repository owner. Do not add AI tools as author or co-author, and do
  not add attribution trailers or generated-by lines to commits or pull request descriptions.
- Don't commit, push, tag or release unless asked.
- Never commit `build/`, `dist/`, `third_party/`, `internal/model/model.bin`, `go.work` or
  `go.work.sum`. Do commit `go.sum`.

## Known pitfalls

- **Link errors for `ggml_*` or `whisper_*` symbols:** `build/whisper/libwhisper.a` is stale or
  missing. Run `make lib`, or delete `build/whisper` and rebuild.
- **`flag provided but not defined`:** the flag isn't in `settings.Fields` or registered in
  `main.go`.
- **Hotkey fires during shortcuts:** check the `dirty` and input-counter logic in
  `tapHotkey.poll` before changing `maxTap`.
