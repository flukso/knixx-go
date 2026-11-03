# Unify ota and nusterm into one `bolt` host tool

> **Status: completed.** Implemented in commit `07827e9` ("unify ota and
> nusterm into bolt") (2026-10-21). Recovered from the planning session's
> transcript: the session reused one plan file for all its plans, so only
> the last one was left on disk.

## Context
The BLE host tools `src/host/ota` and `src/host/nusterm` are two `main`s that
start the same way: enable the adapter, scan for the board by name or
address with `blescan.Find` (`src/host/internal/blescan/blescan.go`), and
connect. They become one command, `bolt`, with the subcommands `bolt ota`
and `bolt term`, so there is one host tool for the board over BLE. The make
targets `make ota` and `make nus` stay, and a new `make bolt` builds
`build/bolt` for use outside make (user's choices).

## Layout
`src/host/ota` and `src/host/nusterm` are replaced by one package `main` in
`src/host/bolt/` (`git mv` where a file maps over, to keep history):

| file | from | contents |
|---|---|---|
| `main.go` | new | package doc, subcommand dispatch, usage, `find` |
| `ota.go` | `host/ota/main.go` | `runOTA`, `link`, `openDFU` |
| `term.go` | `host/nusterm/main.go` | `runTerm`, `read`, `openNUS` |
| `input.go` | `host/nusterm/term.go` | `pending`, `chunks`, `Escape`, `defaultChunk` (unchanged) |
| `input_test.go` | `host/nusterm/term_test.go` | unchanged tests |

`host/internal/blescan` and `host/internal/legacydfu` stay as they are.

## main.go
- No CLI library: `os.Args[1]` picks the subcommand, each with its own
  `flag.NewFlagSet("bolt <cmd>", flag.ExitOnError)` and the same flags as
  today:
  - `ota`: `-zip` (default `build/knixx-dfu.zip`, since make now runs bolt
    from the repository root), `-name AdaDFU`, `-addr`, `-wait 60s`.
  - `term`: `-name knixx`, `-addr`, `-wait 30s`.
- No argument, `help`, `-h` or an unknown name prints a usage listing both
  subcommands to stderr; unknown names exit with status 2.
- Errors print as `bolt ota: ...` / `bolt term: ...` and exit with status 1.
- Shared `find(name, addr string, wait time.Duration, hint string)
  (*bluetooth.Adapter, bluetooth.ScanResult, error)`: enables
  `bluetooth.DefaultAdapter`, prints `waiting for <target> (run <hint> on the
  knixx console)...` to stderr, calls `blescan.Find`. Each subcommand then
  connects itself, since `term` must set its connect handler between the
  scan and `adapter.Connect` (it matches the disconnect on `found.Address`).
- Status messages go to stderr in both subcommands. For `term` they already
  do (stdout is the board's output); `ota`'s `fmt.Printf` lines (waiting,
  connecting, ATT MTU, progress, done) move to stderr too.
- `open` exists in both tools today; they become `openDFU` and `openNUS`.
- Package doc: what bolt is (thunder's host tool over BLE), the two
  subcommands, BlueZ on Linux and CoreBluetooth on macOS. Per-subcommand
  notes from the old package docs move to the `runOTA`/`runTerm` comments.

## Makefile
- New phony `bolt` target: `cd src && go build -o ../build/bolt ./host/bolt`
  (Go's build cache makes the repeat cheap).
- `ota: $(DFU) bolt` runs `build/bolt ota -zip $(DFU) $(if $(ADDR),-addr $(ADDR))`.
- `nus: bolt` runs `build/bolt term $(if $(ADDR),-addr $(ADDR))`.
- Add `bolt` to `.PHONY`; comments name bolt. `build/` is already ignored.
- `FW_SRC` already skips `src/host`.

## README
- BLE console: `make nus` runs `bolt term` (instead of `go run ./nusterm`).
- Over-the-air update: `make ota` runs `bolt ota`.
- Build list: add `make bolt   # build build/bolt, the host tool for ota and term`.
- Code layout: one row `host/bolt` replaces the `host/ota` and `host/nusterm`
  rows; the `blescan` row says "for bolt".
- `git grep -nE 'nusterm|host/ota|\./ota\b'` finds nothing left afterwards.

## Verification
- `make test` passes (vet + tests, including `host/bolt` and the helpers).
- `make bolt`; `build/bolt`, `build/bolt help`, `build/bolt foo` (exit 2),
  `build/bolt term -h`, `build/bolt ota -h` print the expected usage.
- On the board, as tested on this Mac earlier:
  - `nus on` over USB, then `echo help | make nus` prints the help output;
    the scripted paste, `-addr`, Ctrl-] and `nus off` checks behave as before.
  - `make ota` + `ota` on the console updates the board; `version` shows the
    new build.
- Commit only when the user asks.
