# `sniff on|off` shell command to toggle the KNX hex output

> **Status: completed.** Implemented in commit `1cd0b0d` ("ini-configured
> switches with interactive shell") (2026-10-13). Committed together with
> the configurable switches. Recovered from the planning session's
> transcript: the session reused one plan file for all its plans, so only
> the last one was left on disk.

## Context

The transmit goroutine in `src/ftdi/tx.go` unconditionally prints `> % x` for
every
telegram sent and `< % x` for every chunk read back from the NCN5120. These
lines land in
the middle of the ishell prompt. A `sniff on` / `sniff off` command should let
the user
enable or disable that hex output at runtime.

## Design

### Shared flag: `sync/atomic.Bool`

The shell (main goroutine) writes the flag, the transmit goroutine reads it. A
package-level
`var sniff atomic.Bool` in `src/ftdi/tx.go` is enough: no ordering matters,
and unlike a
message on the `cmds` channel the change applies immediately instead of
queueing behind
pending telegrams. (Host-only code; the nrf target doesn't use this file.)

- Default: **on**, set in `main()` before the goroutine starts, so behaviour
  without the
  command is unchanged.
- Scope: gates both the `>` (TX) and `<` (RX) hex lines. Error messages on
  stderr are
  always shown.
- With sniff off the goroutine keeps calling `d.Read` every 10 ms and discards
  the bytes,
  so the FTDI RX buffer doesn't fill with stale data that would be dumped on
  `sniff on`.

### `src/ftdi/tx.go`

- Add `var sniff atomic.Bool`.
- Add `func dump(prefix string, buf []byte)` that prints `prefix % x` only
  when
  `sniff.Load()`; replace the two `fmt.Printf` calls in `transmit` with it.

### `src/ftdi/shell.go`

- Extract the on/off parsing from `parseSwitch` into
  `parseOnOff(arg string) (on bool, ok bool)` (case-insensitive) and reuse it
  there.
- Add `const sniffUsage = "usage: sniff <on|off>"`.

### `src/ftdi/main.go`

- `sniff.Store(true)` before `go transmit(...)`.
- Register a second command next to `switch`:
  - `sniff on` / `sniff off` → `parseOnOff(c.Args[0])` → `sniff.Store(on)`.
  - `sniff` without arguments prints the current state (`sniff is on`/`off`).
  - anything else prints `sniffUsage`.

## Files

| File | Change |
|---|---|
| `src/ftdi/tx.go` | `sniff` flag, `dump` helper |
| `src/ftdi/shell.go` | `parseOnOff`, `sniffUsage` |
| `src/ftdi/main.go` | default on, `sniff` command |
| `src/ftdi/shell_test.go` | `parseOnOff` cases; existing `parseSwitch` cases must still pass |
| `src/ftdi/tx_test.go` | run the existing transmit test with sniff on and off: telegrams are written to the device either way |

## Verification

- `cd src && gofmt -l ftdi && go vet ./ftdi && go test ./light ./config
  ./ftdi`
- `go build ./ftdi`, then pipe `sniff`, `sniff off`, `sniff`, `sniff bogus`,
  `exit` into the
  binary and check the state/usage replies (opens the adapter but sends
  nothing to the bus).
- On the bus, yours to run: `switch A on` shows `>`/`<` lines; after `sniff
  off` the same
  command switches the light with no hex output; `sniff on` brings the lines
  back.
