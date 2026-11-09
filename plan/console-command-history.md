# Console command history (up/down) on the nRF52840

> **Status: completed.** Implemented in commit `b8480c1` ("command history
> on the console with the up and down keys"), followed by `ba65dae`
> (2026-10-16). Recovered from the planning session's transcript: the
> session reused one plan file for all its plans, so only the last one was
> left on disk.

## Context

The nrf console (`src/nrf/console.go`, `runConsole`) is a hand-rolled line
reader on
USB CDC: it echoes printable bytes, handles backspace/DEL and runs `cli.Exec`
on CR/LF.
It has no history. We want ↑/↓ to cycle through earlier lines, as ishell
already does
on the ftdi host build.

How terminals send the keys (picocom, minicom, screen, `tio`):
- ↑ = `ESC [ A`, ↓ = `ESC [ B`. In application cursor mode they are `ESC O A`
  and
  `ESC O B`.
- Today each of these bytes goes into the line buffer. ESC is dropped, but `[`
  and `A`
  are printable, so pressing ↑ currently inserts `[A` into the line. That is
  an
  existing bug, and the escape decoding below fixes it.

## Approach: a portable line editor package

Move the line editing out of the `tinygo`-only `console.go` into a new
package,
`src/lineedit`. It uses no `machine` import, so `go test` on the host can
unit-test it.
`console.go` keeps only the USB polling, the banner and `cli.Exec`.

```go
package lineedit

const (
    MaxLine    = 80
    HistoryLen = 8 // 8 × 80 B ring + 80 B draft
)

type Editor struct {
    Prompt string
    Out    io.Writer // raw terminal output (echo, redraws)

    line  [MaxLine]byte; n uint8
    draft [MaxLine]byte; draftN uint8 // line being typed before browsing
    hist  [HistoryLen][MaxLine]byte; histN [HistoryLen]uint8
    head, count uint8 // ring: newest entry at head-1
    browse uint8      // 0 = editing draft, k = k-th newest entry
    esc   uint8       // escNone / escStart / escCSI
    lastCR bool
}

// Feed processes one input byte. It returns the completed line when b ends one;
// the slice is valid until the next Feed.
func (e *Editor) Feed(b byte) (line []byte, done bool)
```

Everything is in fixed-size arrays with uint8 indices, so there are no
allocations per
keystroke, in line with CLAUDE.md. The editor costs about 1 KB of RAM.

### Feed behaviour

- **Escape state machine:**
  - On ESC, go to `escStart`.
  - In `escStart`, `[` or `O` moves to `escCSI`, and any other byte returns to
    none.
  - In `escCSI`, parameter bytes 0x30–0x3f are consumed. A final byte
    0x40–0x7e ends
    the sequence. `A` and `B` (with no parameters) act as up and down, and
    every other
    sequence (←/→, `ESC[3~`, …) is swallowed silently.
- **Ctrl-P (0x10) and Ctrl-N (0x0e):** aliases for up and down. They cost one
  line.
- **Up:**
  - Does nothing if `browse == count`.
  - When `browse == 0`, the editor first saves `line` into `draft`.
  - It then sets `browse++`, copies that history entry into `line`, and
    redraws.
- **Down:**
  - Does nothing if `browse == 0`.
  - Otherwise it sets `browse--` and loads either the history entry or, at 0,
    the
    draft. Then it redraws.
- **Redraw:** writes `"\r" + Prompt + line + "\x1b[K"` (erase to end of line),
  which
  every VT100-style terminal supports.
- **CR/LF:**
  - Keeps today's `lastCR` handling of a CR LF pair.
  - The line is added to history if it is non-empty after trimming and differs
    from the
    newest entry, like bash's `ignoredups`.
  - Resets `browse` and `n`, then returns `line[:n], true`.
  - It does not write the newline itself; the caller does that through `crlf`,
    as it
    does today.
- **Editing a recalled line:** works on the `line` copy, so history entries
  never
  change.
- **Backspace/DEL and printable 0x20–0x7e:** behave as today, with echo to
  `Out`.

### `src/nrf/console.go`

`runConsole` keeps the banner-on-first-key logic. Its loop body becomes:

```go
if line, done := ed.Feed(b); done {
    console.Write([]byte{'\n'})
    cli.Exec(cmds, string(line), console)
    console.Write([]byte(prompt))
}
```

The editor is set up as `ed := lineedit.Editor{Prompt: prompt, Out:
machine.Serial}`.
`Out` is the raw serial port, because the editor writes `\r` explicitly and
never
writes `\n`.

The first-key branch still swallows a lone CR/LF. Any other first key,
including an
arrow sequence, goes into `Feed`.

These are removed from `console.go` because they move to `lineedit`:
`maxLineLen`,
`keyBackspace`, `keyDelete` and `lastCR`.

### Not changed

- `ftdi` (ishell already has history), `cli`, `bus`, `config`.
- History lives in RAM only and is not saved to littlefs. Persisting it can be
  a
  follow-up if wanted.

## Files

| File | Change |
|---|---|
| `src/lineedit/lineedit.go` | new: `Editor`, `Feed`, escape decoding, history ring |
| `src/lineedit/lineedit_test.go` | new: host tests |
| `src/nrf/console.go` | use `lineedit.Editor`; drop the inline editing |

## Verification

- `cd src && go vet ./... && go test ./...`. The `lineedit` tests feed byte
  strings and
  check the returned lines and the `Out` buffer:
  - After typing `switch A on\r` and `stat\r`, pressing ↑ shows `stat`, a
    second ↑ shows
    `switch A on`, a third ↑ stays put, ↓ returns to `stat`, and another ↓
    restores the
    typed draft.
  - `ESC O A` works like `ESC [ A`.
  - ←/→ and `ESC[3~` insert nothing.
  - Duplicate and empty lines are not recorded.
  - After more than `HistoryLen` entries, the oldest is dropped.
  - Editing a recalled line leaves the history entry unchanged.
  - A CR LF pair yields one line, and backspace and the 80-byte limit work as
    before.
- `tinygo build -target=feather-nrf52840 ./nrf` succeeds; compare its size
  with the
  current build.
- Hardware (yours): in picocom, run `switch A on` and `stat`, then press ↑↑
  and Enter;
  `switch A on` should run again. Pressing ← should no longer insert `[D`.
