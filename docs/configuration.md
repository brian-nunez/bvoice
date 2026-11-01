# Configuration

bvoice has twelve settings and four command-line-only flags. Every setting can be changed in the
**Settings** tab of the window and passed as a flag when starting the binary.

- [How settings are resolved](#how-settings-are-resolved)
- [Flag syntax](#flag-syntax)
- [Settings reference](#settings-reference)
- [Command-line-only flags](#command-line-only-flags)
- [Storage](#storage)
- [Exit codes](#exit-codes)
- [Examples](#examples)

## How settings are resolved

Each setting's value comes from three layers, later ones winning:

1. **Built-in default** (listed for each setting below).
2. **Saved value** in the settings database, written whenever you change a setting in the window or
   press **Sync**.
3. **Flag** passed on the command line, for the current run only.

Flags are never saved automatically. When a flag differs from the saved value, the window shows a
notice: *"You started bvoice with different options than your saved settings. Do you want to sync
them?"* Hovering over the notice lists each difference (saved value and this run's value). **Sync**
saves all of them and hides the notice.

Changing a setting in the window saves it immediately and applies it to the running app, even if
the same setting was passed as a flag. The window's value then replaces the flag and drops out of the
sync notice.

If a saved value is invalid (for example, a database edited by hand), bvoice falls back to the
default for that setting and prints a warning at startup:

```
bvoice: saved setting sound-volume: want a number from 0 to 1, got "2" (using default)
```

## Flag syntax

- Flags take one or two dashes: `--sounds` and `-sounds` are the same.
- Values are given with `=` or as the next argument: `--hotkey=right_option` or
  `--hotkey right_option`.
- **On/off settings** accept `true`/`false` (also `1`/`0`, `t`/`f`, `TRUE`/`FALSE`). A bare flag
  means `true`. To turn one off, use `=`: `--sounds=false`. `--sounds false` does **not** work, because
  `false` is read as a stray argument.
- Values with spaces need quotes: `--vocab="Kubernetes, PostgreSQL"`, `--mic="MacBook Pro Microphone"`.
- Invalid values, unknown flags and stray arguments stop bvoice before it starts, with exit code 2.

## Settings reference

The **key** is both the flag name and the name stored in the database.

| Key | Window label | Type | Default | Allowed values |
|---|---|---|---|---|
| [`sounds`](#sounds) | Sounds | on/off | `true` | `true`, `false` |
| [`sound-volume`](#sound-volume) | Sound volume | number | `0.5` | `0` to `1` |
| [`hotkey`](#hotkey) | Hotkey (tap) | choice | `left_option` | see below |
| [`window`](#window) | Show window on launch | on/off | `true` | `true`, `false` |
| [`on-top`](#on-top) | Window always on top | on/off | `true` | `true`, `false` |
| [`auto-copy`](#auto-copy) | Copy to clipboard automatically | on/off | `true` | `true`, `false` |
| [`trailing-space`](#trailing-space) | Add trailing space | on/off | `true` | `true`, `false` |
| [`vocab`](#vocab) | Custom vocabulary | text | empty | any text |
| [`mic`](#mic) | Microphone | choice | empty (system default) | a microphone name |
| [`auto-stop`](#auto-stop) | Auto-stop after silence (s) | whole number | `0` (off) | `0` to `3600` |
| [`max-minutes`](#max-minutes) | Max recording length (min) | whole number | `30` | `1` to `240` |
| [`history-limit`](#history-limit) | History entries to keep | whole number | `500` | `1` to `100000` |

### `sounds`

Plays a short sound when listening starts (Tink) and when the transcript is ready (Glass). If the
microphone can't be opened, a third sound (Basso) plays instead of the start sound. No sound plays
when a recording contained no speech.

```sh
./bvoice --sounds=false
```

### `sound-volume`

Volume of the sounds above, from `0` (silent) to `1` (full system volume). Any decimal in that range
is accepted; the window's slider saves two decimal places. Has no effect when `sounds` is off.

```sh
./bvoice --sound-volume=0.25
```

### `hotkey`

The modifier key you tap to start and stop listening.

| Value | Key |
|---|---|
| `left_option` | Left Option (⌥) |
| `right_option` | Right Option |
| `left_command` | Left Command (⌘) |
| `right_command` | Right Command |
| `left_shift` | Left Shift (⇧) |
| `right_shift` | Right Shift |
| `left_control` | Left Control (⌃) |
| `right_control` | Right Control |
| `fn` | Fn / Globe |

A press counts as a tap only when all of these are true:

- the key goes down and back up within 400 ms;
- no other modifier key is held at the same time;
- no other key or mouse button is pressed in between.

So holding the key, using it in a shortcut (⌥←, ⌘C) or typing a special character (⌥e) never starts
dictation. Changing the hotkey in the window takes effect immediately.

Only single modifier keys are supported, because detecting other keys or combinations requires macOS
Input Monitoring permission. If you choose `fn`, macOS may also run its own Fn/Globe action (set in
**System Settings → Keyboard**) on the same tap.

```sh
./bvoice --hotkey=right_option
```

### `window`

Whether the floating window opens when bvoice starts. When off, bvoice starts with only the menu bar
icon; click the icon to open the window. Applies at launch only.

```sh
./bvoice --window=false
```

### `on-top`

Keeps the floating window above other windows. When off, it behaves like a normal window and can go
behind others. Applies immediately when changed in the window. In either case the window never takes
keyboard focus from the app you are dictating into.

```sh
./bvoice --on-top=false
```

### `auto-copy`

Copies each transcript to the clipboard as soon as it is ready. When off, transcripts are only saved
to history; click an entry in the **History** tab to copy it.

```sh
./bvoice --auto-copy=false
```

### `trailing-space`

Adds a space after the text whenever bvoice copies a transcript, both automatically and from history.
That way, pasting several dictations in a row doesn't run the words together. The space is not stored
in history or included in exports.

```sh
./bvoice --trailing-space=false
```

### `vocab`

Names, product terms and jargon that Whisper should expect, such as `Kubernetes, PostgreSQL, bvoice`.
Leading and trailing spaces are removed. This text is passed to Whisper as a prompt for every part of
the recording, along with the end of what has already been transcribed. It helps with spelling and
capitalization but is a hint, not a guarantee.

Keep it short: a few dozen words at most. Whisper's prompt has a limited length, and a long
vocabulary leaves less room for the preceding text. Writing the terms the way they should appear in
the transcript (with the right capitalization and punctuation) works best.

```sh
./bvoice --vocab="Kubernetes, PostgreSQL, Terraform, bvoice"
```

### `mic`

The input device to record from, by its exact name as listed in the window's **Microphone** menu
(the same names as in **System Settings → Sound → Input**). Empty means the system default input,
which follows changes you make in System Settings.

The name is checked when you start dictating, not at launch. If the device isn't connected, the
window shows *Could not open the microphone: microphone "…" not found* and the error sound plays.

```sh
./bvoice --mic="MacBook Pro Microphone"
./bvoice --mic=""          # system default
```

### `auto-stop`

Stops listening automatically after this many seconds without speech. `0` turns it off. The silence
is counted from the last detected speech, or from the start if nothing has been said yet. An
auto-stop behaves exactly like tapping the hotkey: the transcript is copied and saved, and the window
shows *Stopped after N s of silence.*

```sh
./bvoice --auto-stop=10
```

### `max-minutes`

A safety limit on the length of a single recording, in minutes. When it is reached, listening stops
as if you had tapped the hotkey, and the window shows *Stopped at the N-minute limit.*

```sh
./bvoice --max-minutes=60
```

### `history-limit`

The most transcriptions to keep. When a new transcription would go over the limit, the oldest ones
are deleted. Lowering the limit in the window deletes the excess right away. The **History** tab
shows the newest 200 entries (or the newest 200 matches when searching); **Export…** writes all of
them.

```sh
./bvoice --history-limit=2000
```

## Command-line-only flags

These are not settings: they are never saved and don't appear in the window.

### `--data-dir=PATH`

The folder holding the settings and history database. The default is
`~/Library/Application Support/bvoice`. The folder is created if it doesn't exist (readable only by
you). Use it to keep separate profiles, or a throwaway one for testing:

```sh
./bvoice --data-dir=/tmp/bvoice-test
```

### `--verbose`

Writes a detailed log to the terminal (stderr) with timestamps: model load time, the data folder,
the microphone in use, timing for each transcribed segment, and the delay between stopping and the
text being ready. Also turns on whisper.cpp's own log output.

```
09:15:02.512301 bvoice: loaded model small.en-q5_1 in 412ms; data in /Users/me/Library/Application Support/bvoice
09:15:06.004118 bvoice: listening (mic "")
09:15:11.880932 bvoice: chunk 4.6s at 0ms transcribed in 384ms
09:15:13.341275 bvoice: done: 118 chars from 7.3s of audio, ready 702ms after stop
```

### `--version`

Prints the version and the embedded model, then exits:

```
bvoice v0.1.0 (model small.en-q5_1)
```

### `--help`

Lists every flag with its description and default, then exits. `-h` works too.

## Storage

Settings and history are stored in SQLite at `<data-dir>/bvoice.db`. SQLite also keeps
`bvoice.db-wal` and `bvoice.db-shm` next to it while bvoice is running. Settings are stored as text
under the keys listed above, so the database can be inspected with any SQLite tool:

```sh
sqlite3 ~/Library/Application\ Support/bvoice/bvoice.db 'SELECT key, value FROM settings'
```

Binaries built with different models use the same folder by default, so they share settings and
history. Each history entry records which model produced it, the recording length and the delay
after stopping.

To reset everything, quit bvoice and delete the folder.

## Exit codes

| Code | Meaning |
|---|---|
| `0` | Quit normally, or `--help`/`--version` finished |
| `1` | Couldn't start: data folder or database unavailable, model failed to load, audio or hotkey setup failed, unsupported OS |
| `2` | Invalid command line: unknown flag, invalid value or stray argument |

## Examples

```sh
# Quiet mode with a different hotkey
./bvoice --sounds=false --hotkey=right_command

# Hands-free: stop after 5 seconds of silence, never more than 10 minutes
./bvoice --auto-stop=5 --max-minutes=10

# Start hidden in the menu bar, with a USB microphone and project vocabulary
./bvoice --window=false --mic="USB Audio Device" --vocab="Terraform, GKE, Helm"

# Separate profile for testing, with detailed logs
./bvoice --data-dir=/tmp/bvoice-test --verbose

# Only keep history; copy manually when needed
./bvoice --auto-copy=false
```
