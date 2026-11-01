# bvoice — build plan

bvoice is a local, private speech-to-text dictation tool for macOS. Tap a key, talk, tap again, and the
text is on your clipboard about a second later — whether you said one sentence or several pages.
It ships as **one self-contained binary per Whisper model**, with a small menu bar icon and a
dismissable floating window.

This document is the full spec. Everything in it was decided with the owner (Brian); items marked
**Decided** are not open for reinterpretation. Anything not covered here should be asked about,
not assumed.

---

## 1. Hard constraints (Decided)

1. **One binary, nothing else.** Run as `./bvoice <args>`. The Whisper model is embedded in the binary.
   No model files, dylibs, frameworks or helper processes beside it. (Shelling out to tools that ship
   with macOS is acceptable but prefer native APIs — see §8.)
2. **No permissions or settings to enable it.** It must work without:
   - moving it into `/Applications` or wrapping it in a `.app` bundle,
   - Accessibility, Input Monitoring, Full Disk Access, Login Items or any other System Settings change.
   The **only** permission prompt allowed is macOS's one-time microphone consent dialog, which no app
   can avoid. (When launched from a terminal that already has mic access, there is no prompt at all.)
3. **No Docker.** Docker on macOS can't reach the mic, keyboard or clipboard.
4. **macOS first**, on an **Apple Silicon M3**. Linux support comes later; the code must be structured so
   adding it does not require a large refactor (§6).
5. **English only.**
6. **Language: Go.**

### Consequences of constraint 2 (Decided)
- **No auto-paste.** Typing into another app needs Accessibility. Text goes to the **clipboard**; the
  user presses ⌘V.
- **Hotkeys are modifier-key taps only.** Global key combos via event taps need Input Monitoring. Instead
  bvoice **polls the modifier-key state** (`CGEventSourceFlagsState`), which needs no permission.
  ⚠️ This must be verified on the M3 first (milestone 1). If polling turns out to require a
  permission on current macOS, stop and tell the owner before working around it.
- **No "launch at login"** (it creates a Login Item visible in System Settings).

---

## 2. User experience

### Dictation
- **Tap Left Option** (alone, quickly) to **start listening**. **Tap it again to stop.** Toggle, not hold.
- A tap counts only if: Left Option went down and up within ~400 ms, no other modifier was held during
  it, and no regular key was pressed during it (check `CGEventSourceCounterForEventType(...,
  kCGEventKeyDown)` before/after). This keeps ⌥-shortcuts and ⌥-characters from triggering it.
- While listening, audio is **transcribed in the background at each natural pause** (§4), so when the
  user stops, only the last few seconds remain. **Target: text ready ≈1 s after stopping, regardless of
  length** (one sentence up to multiple pages of a Word document).
- On finish: the full text is copied to the clipboard (if auto-copy is on), saved to history, and the
  "done" sound plays.

### Sounds (Decided: on by default, toggleable)
- Start listening: short chime. Text ready: a different short chime. Use built-in macOS system sounds
  (e.g. Tink / Glass) via `NSSound`, at the configured volume.

### Menu bar icon
- Always present while bvoice runs (no Dock icon — `NSApplicationActivationPolicyAccessory`).
- Icon reflects state: **idle**, **listening**, **finishing transcription**. Use SF Symbols
  (e.g. `waveform`, `mic.fill`, `ellipsis`).
- Click → show/hide the floating window. Its menu also has Quit.

### Floating window (Decided: small, dismissable)
A compact `NSPanel` (non-activating, utility style) so it never steals focus from the app being
dictated into. Closing it only hides it; the menu bar icon brings it back. `Esc` hides it.
Contents:
1. **Header:** state (idle / listening with elapsed time / transcribing), **current model name**,
   bvoice version. Model is read-only — switching models = quit and run a different binary (§5).
2. **Sync notice** (only when launch flags differ from saved settings, §3.3):
   *"You started bvoice with different options than your saved settings. Do you want to sync them?"*
   with a **Sync** button. Shown inside this window — not the terminal, not a macOS notification.
3. **History:** newest first, with a **search** field. Each entry shows time, model, a preview of the
   text; clicking an entry copies its full text to the clipboard. Buttons: **Export…** (Markdown/text via
   `NSSavePanel`) and **Clear history** (with confirmation).
4. **Settings** (§3): all UI-editable options, saved immediately to SQLite.
5. **Quit** button.

---

## 3. Options

### 3.1 Option list (Decided)

| Option | Default | In UI | Flag | Notes |
|---|---|---|---|---|
| Sounds on/off | on | toggle | `--sounds=true\|false` | |
| Sound volume | 0.5 | slider | `--sound-volume=0..1` | |
| Hotkey | `left_option` | picker | `--hotkey=NAME` | names in §3.2 |
| Show window on launch | on | toggle | `--window=true\|false` | |
| Window always on top | on | toggle | `--on-top=true\|false` | |
| Auto-copy to clipboard | on | toggle | `--auto-copy=true\|false` | off = copy from history only |
| Trailing space | on | toggle | `--trailing-space=true\|false` | appended to each transcript |
| Custom vocabulary | empty | multi-line text | `--vocab="a, b, c"` | names/jargon fed to Whisper as the initial prompt |
| Microphone | system default | picker | `--mic=NAME` | lists capture devices |
| Auto-stop after silence | off (0) | number (seconds) | `--auto-stop=SECONDS` | 0 = off |
| Max recording length | 30 min | number (minutes) | `--max-minutes=N` | safety cap; auto-stops |
| History limit | 500 | number | `--history-limit=N` | oldest entries pruned |
| Clear history | — | button | — | |
| Export history | — | button | — | |
| Data folder | `~/Library/Application Support/bvoice/` | — | `--data-dir=PATH` | flag only |
| Verbose logging | off | — | `--verbose` | flag only; logs to stderr |
| Version / model | — | shown read-only | `--version` | prints version + embedded model, exits |
| Help | — | — | `--help` | |

### 3.2 Hotkey names
Modifier taps only (see §1): `left_option` (default), `right_option`, `left_command`, `right_command`,
`left_shift`, `right_shift`, `left_control`, `right_control`, `fn`.
Detect sides with the device-dependent bits in the flags word:
`L-ctrl 0x01, L-shift 0x02, R-shift 0x04, L-cmd 0x08, R-cmd 0x10, L-opt 0x20, R-opt 0x40, R-ctrl 0x2000`,
`fn = kCGEventFlagMaskSecondaryFn (0x800000)`.

### 3.3 Flags vs saved settings (Decided)
- Effective settings = saved settings (SQLite) **overridden by any flags passed** — for **this run only**.
  Flags are never written to the database automatically.
- If any passed flag differs from the saved value, show the **sync notice** (§2). **Sync** writes the
  flag values to SQLite and hides the notice.
- Changes made in the UI are saved immediately and also apply to the running session.

---

## 4. Transcription pipeline

- **Audio capture:** 16 kHz, mono, float32 via `github.com/gen2brain/malgo` (miniaudio, compiled in;
  uses CoreAudio). Open the device only while listening, so the orange mic indicator is only on then.
- **Chunker (background "live" transcription, Decided):**
  - Simple energy VAD on ~30 ms frames.
  - Cut a chunk when there's a pause of ≥ ~600 ms and the chunk is ≥ ~2 s; force a cut at ~25 s
    (Whisper's window is 30 s), preferably at the quietest recent frame.
  - Chunks go to a single worker that transcribes them in order while the user keeps talking.
  - Each chunk's prompt = custom vocabulary + the tail of the previously transcribed text, for continuity
    of punctuation/capitalization across chunks.
  - Skip chunks that are essentially silence (don't send them to Whisper — avoids hallucinations).
  - On stop: flush the final partial chunk, wait for the worker, join all chunk texts, done.
  - Tune constants (pause length, min/max chunk) as internal constants first; they are **not** user options.
- **Whisper:** whisper.cpp via cgo, statically linked, **Metal enabled with the Metal library embedded**
  (`GGML_METAL=ON`, `GGML_METAL_EMBED_LIBRARY=ON`), OpenMP off. Load the model from memory with
  `whisper_init_from_buffer_with_params`. Greedy decoding, `language="en"`, `no_timestamps`,
  `suppress_blank`, `suppress_nst`, threads = min(NumCPU, 8). Strip `[BLANK_AUDIO]`, `(music)` etc.
- **Auto-stop after silence / max length:** handled by the recorder; behaves exactly like a manual stop.

---

## 5. Models (Decided)

- **One model per binary.** The model is chosen at build time and embedded.
- Build both to compare:
  - `small.en` → `ggml-small.en-q5_1.bin` (~190 MB)
  - `large-v3-turbo` → `ggml-large-v3-turbo-q5_0.bin` (~550 MB)
- Any other whisper.cpp model name must also work with `make MODEL=<name>` for further testing
  (e.g. `base.en-q5_1`, `medium.en-q5_0`). Source:
  `https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-<name>.bin`.
- Switching models = quit one binary, run another. No model switching inside the app.
- The model name is embedded via ldflags and shown in the window and `--version`.

---

## 6. Architecture (must stay extensible — Decided)

The core knows nothing about AppKit. A Linux UI (or a different toolkit) later = one new package
implementing the interfaces below, not a rewrite.

```
cmd/bvoice/            main: flags, wiring, version
internal/app/          Controller: state machine (idle → listening → finishing), owns the session,
                       talks to everything below only through interfaces
internal/vad/          energy VAD + chunker (pure Go, unit-tested)
internal/whisper/      cgo wrapper over whisper.cpp (load from buffer, transcribe, cleanup)
internal/model/        //go:embed model.bin  (file copied in by the Makefile, git-ignored)
internal/settings/     Settings struct, defaults, validation, flag overlay, diff (for the sync notice)
internal/store/        SQLite (github.com/mattn/go-sqlite3, compiled in): settings + history, migrations
internal/history/      the history Entry type (no dependencies)
internal/textproc/     transcript cleanup + decoder prompts (pure Go)
internal/audio/        Recorder interface (no dependencies)
  mic/                 miniaudio implementation
internal/platform/     interfaces: Hotkey, Clipboard, Sounds; platform_darwin.go implements them with
                       CGEventSourceFlagsState polling, NSPasteboard, NSSound (cgo + Objective-C)
internal/ui/           UI interface + view models (status, history rows, controls, notice)
  appkit/              AppKit: NSStatusItem + NSPanel (cgo + Objective-C, JSON bridge)
```

- Platform-specific files use build tags (`//go:build darwin`). Non-darwin builds compile with stub
  implementations that return a clear "not supported yet" error, so `go build`/`go test` of the core
  works on Linux today.
- **UI contract:** the UI receives state updates (`SetState`, `SetHistory`, `ShowSyncNotice`,
  `SetSettings`) and sends user actions back through an `Actions` interface
  (`ToggleRecording`, `UpdateSetting`, `SearchHistory`, `CopyEntry`, `ExportHistory`, `ClearHistory`,
  `SyncFlags`, `Quit`). The UI owns the main thread (`UI.Run` blocks); everything else runs in
  goroutines and UI updates are dispatched to the main thread.
- AppKit needs the main OS thread: `runtime.LockOSThread()` in `init` of the darwin UI package.

### SQLite schema (in `<data-dir>/bvoice.db`)
```sql
CREATE TABLE schema_version (version INTEGER NOT NULL);
CREATE TABLE settings (key TEXT PRIMARY KEY, value TEXT NOT NULL, updated_at INTEGER NOT NULL);
CREATE TABLE transcriptions (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  created_at INTEGER NOT NULL,      -- unix ms
  model TEXT NOT NULL,
  audio_ms INTEGER NOT NULL,        -- length of the recording
  latency_ms INTEGER NOT NULL,      -- stop → text ready (to compare models)
  text TEXT NOT NULL
);
CREATE INDEX transcriptions_created ON transcriptions(created_at DESC);
```
History is shared across all model binaries (they use the same data folder), which makes comparing
models easy. Writing to `~/Library/Application Support/` needs no permission.

---

## 7. Build & release (Decided)

Built **on the M3**; nothing is downloaded through a browser, so Gatekeeper doesn't interfere.

Prerequisites: Xcode Command Line Tools (`make`, `clang`), Go ≥ 1.24, `gh` for releases. **No CMake** (Decided):
the Makefile compiles whisper.cpp directly with rules mirroring its CMake build.

`Makefile` targets:
- `make deps` — fetch whisper.cpp at a **pinned commit** into `third_party/whisper.cpp` and compile it
  into `build/whisper/libwhisper.a` with plain make rules (same sources/defines as its CMake build,
  Metal + Accelerate on macOS, Metal shaders embedded via `.incbin`), then resolve Go modules.
- `make model MODEL=x` — download `ggml-x.bin` into `build/models/` (cached).
- `make build MODEL=x` — copy the model into `internal/model/model.bin`, `go build` with
  `-ldflags "-X main.version=… -X main.modelName=x"` → `dist/bvoice-x`.
- `make all` — builds `small.en-q5_1` and `large-v3-turbo-q5_0`.
- `make test` — `go test ./...` (core tests run on any OS).
- `make release TAG=vX.Y.Z` — `git tag`, push the tag, `gh release create` with all `dist/bvoice-*`
  binaries attached.
- `make clean`.

Binary names: `dist/bvoice-small.en-q5_1`, `dist/bvoice-large-v3-turbo-q5_0` (rename to `bvoice` locally
if preferred).

---

## 8. Milestones

1. **Prove the risky bits on the M3** with a tiny spike binary: modifier-key polling works with zero
   permissions; mic capture works (only the mic prompt); NSStatusItem works from an unbundled binary.
2. **Scaffold + build system:** layout, Makefile, pinned whisper.cpp, embedded model, `--version`.
3. **Core:** whisper wrapper, VAD/chunker, settings + flags + diff, SQLite store — with unit tests.
4. **macOS platform layer:** hotkey tap detection, recorder, clipboard, sounds; headless end-to-end
   dictation works from the terminal.
5. **UI:** menu bar icon, floating panel, history/search/export/clear, settings, sync notice.
6. **Polish + release:** latency check (≈1 s after stop for long dictations on both models), README,
   `make release`.

## 9. Testing

- Unit (any OS): chunker on synthetic audio (speech/silence patterns, forced cuts), text cleanup,
  settings defaults/validation/flag overlay/diff, store migrations + history limit + search.
- Manual on the M3 (checklist in README): tap detection doesn't fire on ⌥-shortcuts; no permission
  prompts other than mic; 1-sentence and 10-minute dictations; latency after stop; clipboard contents;
  sounds toggle/volume; window hide/show/always-on-top; history search/copy/export/clear; flags +
  sync notice; both model binaries.

## 10. Out of scope (for now)

- Phonon text-to-speech (planned as a later step).
- Linux implementation (structure is ready for it).
- Auto-paste, key-combo hotkeys, launch at login (blocked by the no-permissions constraint).
- In-app model switching.
