# Roadmap

Planned work, in rough priority order. Each item lists its scope, the constraints it must respect,
and open questions to settle before starting. The [invariants](../AGENTS.md#invariants) apply to all
of it: one self-contained binary, no special permissions, fully local, platform-independent core.

## 1. First release

Publish the first versioned GitHub release with both default binaries.

- Run the manual test checklist in the [README](../README.md#manual-test-checklist) on macOS.
- `make release TAG=v0.1.0` after the release branch is merged.

## 2. Compare Whisper models

Find the best accuracy and speed trade-off for dictation by building more binaries
(`make build MODEL=<name>`) and comparing them on the same recordings.

- Candidates: `base.en-q5_1`, `small.en-q5_1`, `medium.en-q5_0`, `large-v3-turbo-q5_0`.
- History already records each entry's model, recording length and time from stop to text ready,
  which gives the latency side of the comparison.
- Outcome: decide which models `make all` builds by default and update `MODELS` in the `Makefile`.

## 3. Text-to-speech with Phonon

Add speech output using [Phonon](https://huggingface.co/collections/gradium/phonon), Gradium's
on-device text-to-speech models. Phonon streams 24 kHz mono audio and runs on the CPU.

Planned features:

- **Read aloud:** a hotkey tap speaks the text on the clipboard. Reading the current selection
  directly needs Accessibility permission, so the flow is copy (⌘C), then tap.
- **Type and speak:** a hotkey opens a small text box in the bvoice window; pressing Enter speaks the
  text.
- **Stop speaking:** a hotkey tap stops playback. Starting dictation also stops playback.
- Settings for the voice, speaking speed if supported, and the hotkeys, added through
  `settings.Fields` like every other option.

Constraints:

- **Single binary.** The Phonon runtime and model weights must be compiled into the executable, the
  same way whisper.cpp and the Whisper model are. The runtime ships as a Rust crate with a C API
  (`ptts.h`), which Go can link through cgo as a static library.
- **License.** `phonon-31m-en` is Apache 2.0 (about 65 MB at q8). The 40M models are CC BY-NC 4.0 and
  can't be used commercially. The model repositories require accepting their terms on Hugging Face
  before download, so the `Makefile` needs a token-aware download step.
- **Hotkeys.** Only modifier-key taps are available without Input Monitoring. With dictation on Left
  Option, read-aloud and stop need other modifiers or tap patterns (for example a double tap).
- **Audio output** goes through miniaudio, which is already linked for capture.

Open questions:

- Which modifier keys or tap patterns map to read aloud, type and speak, and stop.
- Whether a TTS-enabled binary is a separate build flavor (`make build MODEL=... TTS=1`) or always
  included, given the extra size.
- Which voice ships as the default.

## 4. Linux support

Run bvoice on Linux with the same core. See
[Adding a platform](architecture.md#adding-a-platform) for the extension points.

Needed:

- `internal/platform/platform_linux.go`: modifier-tap hotkey, clipboard and sounds.
- A UI package implementing `ui.UI` (tray icon and window), returned from `cmd/bvoice/ui_linux.go`.
- Linux link flags in `internal/whisper/whisper.go` and a CPU (optionally Vulkan or CUDA) build of
  whisper.cpp in the `Makefile`.
- A Linux default for the data folder (`os.UserConfigDir`, usually `~/.config/bvoice`) documented in
  [configuration.md](configuration.md).

Open questions:

- Global modifier detection differs between X11 (key state can be polled) and Wayland (generally not
  available to regular apps without a portal or compositor support). Which display servers to
  support, and how to detect a tap on each without elevated permissions.
- Clipboard access on Wayland versus X11.
- UI toolkit for the tray icon and window.
- Release artifacts: `make release` builds on the host OS only, so Linux binaries need a Linux build
  host or CI.
