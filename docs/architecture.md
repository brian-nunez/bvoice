# Architecture

bvoice is a local speech-to-text dictation tool. macOS is the supported platform; the core is
platform-independent, and platform-specific code sits behind interfaces (see
[Adding a platform](#adding-a-platform)). Tapping a modifier key starts listening;
tapping it again stops, and the transcript is copied to the clipboard. Each binary embeds one Whisper
model and runs entirely on the device.

## Design constraints

- **Single binary.** The Whisper model, whisper.cpp, its Metal shaders and SQLite are all compiled
  into the executable. Nothing else ships beside it.
- **No special permissions.** On macOS, bvoice does not need Accessibility, Input Monitoring or any
  other System Settings change. The only prompt is the microphone consent.
- **Clipboard output.** Typing into other apps would require Accessibility, so transcripts go to the
  clipboard instead.
- **Modifier-tap hotkeys.** Global key combinations need Input Monitoring. bvoice instead polls the
  modifier-key state with `CGEventSourceFlagsState`, which needs no permission.
- **English only.** The default builds use English-only (`.en`) or multilingual models with the
  language fixed to English.

## User experience

### Dictation

- A tap of the hotkey (default **Left Option**) toggles listening on and off.
- A tap counts only when the key goes down and up within 400 ms, no other modifier is held, and no
  key or mouse button is pressed in between. This keeps ⌥-shortcuts and ⌥-characters from triggering
  it.
- While listening, audio is transcribed in the background at each pause, so only the last few seconds
  remain when listening stops. Text is typically ready about a second after stopping, regardless of
  length.
- When finished, the transcript is copied to the clipboard (if auto-copy is on), saved to history, and
  a sound plays (if sounds are on).

### Menu bar icon and window

- The menu bar icon shows the state: idle, listening, or finishing. bvoice has no Dock icon.
- Clicking the icon shows or hides a small floating panel. Right-clicking opens a menu with show/hide,
  start/stop and quit.
- The panel never takes focus from the app being dictated into. Closing it or pressing `Esc` only
  hides it.
- Panel contents:
  - **Header:** state, elapsed time while listening, embedded model and version.
  - **Sync notice:** shown when launch flags differ from saved settings, with a **Sync** button that
    saves them.
  - **History tab:** newest first, with search. Clicking an entry copies it. Includes **Export…**
    (Markdown) and **Clear History…** (with confirmation).
  - **Settings tab:** every option below; changes are saved immediately.

## Options

bvoice has twelve settings, each editable in the window and available as a flag, plus four
command-line-only flags (`--data-dir`, `--verbose`, `--version`, `--help`). Values resolve as
default, then saved value, then flag (for the current run only). When flags differ from saved
values, the window offers to sync them. See [configuration.md](configuration.md) for the full
reference.

All options are defined once in `internal/settings`, which drives defaults, validation, flags, the sync
diff and the UI controls.

## Transcription pipeline

1. **Capture.** 16 kHz mono float32 audio through miniaudio (`github.com/gen2brain/malgo`, CoreAudio
   on macOS). The device is open only while listening.
2. **Chunking** (`internal/vad`). An energy-based voice detector on 30 ms frames, with a noise floor
   equal to the quietest frame in the last two seconds. A chunk ends at a pause of at least 600 ms once
   it is at least 2 s long, or is cut at 25 s at the quietest recent frame. Chunks with under 300 ms of
   speech are skipped, which avoids Whisper hallucinating on silence.
3. **Transcription** (`internal/whisper`). A single worker transcribes chunks in order while recording
   continues. Each chunk's prompt is the custom vocabulary plus the tail of the text so far, which keeps
   spelling and punctuation consistent across chunks. Decoding is greedy, English, without timestamps.
4. **Finish.** On stop, the last partial chunk is flushed, the worker drains, and the chunk texts are
   cleaned (`[BLANK_AUDIO]`, `(music)` and similar markers removed) and joined.

Auto-stop after silence and the maximum length behave exactly like a manual stop.

## Code layout

```
cmd/bvoice/          entry point: flags, wiring, per-OS UI selection
internal/app/        controller: state machine (idle → listening → finishing) and glue
internal/vad/        pause detection and chunking (pure Go)
internal/whisper/    cgo wrapper over whisper.cpp; loads the model from memory
internal/model/      the embedded model (internal/model/model.bin, copied in at build time)
internal/settings/   option table: defaults, validation, flags, sync diff
internal/store/      SQLite settings and history (github.com/mattn/go-sqlite3)
internal/history/    history entry type
internal/textproc/   transcript cleanup and decoder prompts
internal/audio/      Recorder interface; audio/mic is the miniaudio implementation
internal/platform/   Hotkey, Clipboard and Sounds interfaces; platform_darwin.go implements them
internal/ui/         UI contract and view models; ui/appkit is the AppKit implementation
```

The controller depends only on interfaces (`audio.Recorder`, `platform.Services`, `ui.UI`, and small
`Transcriber` and `Store` interfaces), so it is tested with fakes and is independent of the OS.

### UI contract

`ui.UI` receives view models (`SetStatus`, `SetHistory`, `SetControls`, `SetSyncNotice`, `SetOnTop`)
and reports user actions through `ui.Actions` (`ToggleRecording`, `SetSetting`, `Search`,
`CopyEntry`, `Export`, `ClearHistory`, `SyncFlags`, `Quit`). `Run` owns the main thread; everything
else runs in goroutines. The AppKit implementation passes data to Objective-C as JSON, so the
Objective-C side has no knowledge of Go types.

### Adding a platform

A new OS needs:

1. `internal/platform/platform_<os>.go` implementing `New()` with a hotkey, clipboard and sounds.
2. A UI package implementing `ui.UI`, returned from `cmd/bvoice/ui_<os>.go`.
3. Any OS-specific link flags in `internal/whisper/whisper.go` and OS branches in the `Makefile`.

The core packages build and test on any OS; unsupported platforms return a clear error at
startup.

## Data

Settings and history are stored in `<data-dir>/bvoice.db`:

```sql
CREATE TABLE schema_version (version INTEGER NOT NULL);
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at INTEGER NOT NULL);
CREATE TABLE transcriptions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  created_at INTEGER NOT NULL,      -- unix ms
  model TEXT NOT NULL,
  audio_ms INTEGER NOT NULL,        -- recording length
  latency_ms INTEGER NOT NULL,      -- time from stop to text ready
  text TEXT NOT NULL
);
CREATE INDEX transcriptions_created ON transcriptions(created_at DESC);
```

Binaries built with different models share the database, so their history and latency can be
compared side by side.

## Models

Each binary embeds one ggml Whisper model, chosen at build time with `make build MODEL=<name>`. Models
are downloaded from `https://huggingface.co/ggerganov/whisper.cpp`. `make all` builds the default set:

| Model | Binary size | Notes |
|---|---|---|
| `small.en-q5_1` | ~195 MB | fast, accurate for most dictation |
| `large-v3-turbo-q5_0` | ~564 MB | most accurate |

Switching models means running a different binary.

## Build

whisper.cpp is fetched at a pinned commit and compiled by the `Makefile` into a static library, with
Metal and Accelerate on macOS and the Metal shaders embedded through `.incbin`. Building requires
`make`, the Xcode Command Line Tools and Go. See the README for the targets.

## Testing

- **Unit tests** (any OS): the controller with fakes (background transcription, auto-stop, flags and
  sync, copy, search, quit), chunking on synthetic audio (including background noise), settings, the
  store, and text cleanup. `internal/whisper` also transcribes whisper.cpp's sample clip when
  `BVOICE_TEST_MODEL` points to a model file (`make test` sets it).
- **Manual checks** on macOS are listed in the README.

## Not supported

- Typing directly into other apps, key-combination hotkeys and launch at login, since each needs a
  permission or a System Settings change.
- Switching models inside a running binary.
