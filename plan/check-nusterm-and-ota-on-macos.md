# Check nusterm (a8488d1) and ota on macOS

> **Status: completed.** Implemented in commit `288b690` ("make ota works on
> macOS too"), checking `a8488d1` (the BLE console terminal) and `cc507be`
> on macOS (2026-10-20). Recovered from the planning session's transcript:
> the session reused one plan file for all its plans, so only the last one
> was left on disk.

## Context
Commit a8488d1 added `nusterm` (`make nus`), a terminal on the board's BLE
console (NUS) through `tinygo.org/x/bluetooth` v0.16.0, and moved the BLE
scan from `ota` into `src/internal/blescan`. It was written and tested on
Linux (BlueZ) only. On macOS the same code goes through CoreBluetooth (cgo,
`github.com/tinygo-org/cbgo`). cc507be later tuned the firmware connection
interval to 15 ms "the shortest macOS accepts" and coalesced typed keys in
`src/nusterm/term.go`. Goal: confirm `make nus` and `make ota` work on this
Mac (arm64, Go 1.27.3, TinyGo 0.43.0, Command Line Tools present, system
make 3.81), and fix only what breaks.

Already checked (read-only):
- `src/go.sum` has `cbgo v0.0.4` and `x/term`; darwin files exist in the
  bluetooth module, so it should compile without go.mod changes.
- darwin `Write` is write-with-response with a 10 s timeout, as nusterm needs.
- darwin calls the connect handler with `false` on disconnect, with the
  peripheral UUID as address, so nusterm's `[disconnected]` path should work.
- darwin `GetMTU` returns `MaximumWriteValueLength(false)` = ATT MTU − 3
  (already the payload). `nusterm/main.go` (`size = mtu - 3`) and
  `ota/main.go` `PacketSize()` (`mtu - 3`) subtract 3 again: writes are 3
  bytes short of the maximum on macOS. Harmless; note it, fix only if wanted.
- The top-level `Makefile` uses no make-4-only syntax for `nus`/`ota`/`test`/`flash`.

## Steps

1. **Host build and tests** (in `src/`):
   - `go vet ./... && go test ./...` (= `make test`); `nrf` is `//go:build tinygo`,
     so it is skipped. Covers `internal/blescan` and `nusterm` unit tests.
   - `go build -o /dev/null ./nusterm ./ota` to confirm the cgo/CoreBluetooth build.
2. **Flash HEAD** over USB: `make flash` (board on USB). Confirm the banner
   shows the HEAD commit on the USB console.
3. **nusterm, end to end** (run in the user's own terminal: Bluetooth needs a
   TCC permission prompt for the terminal app, Ghostty, on first run):
   1. On the USB console: `nus on`.
   2. `make nus`: expect "waiting for knixx", "connecting to <UUID>",
      "connected", then the banner/prompt from the initial CR.
   3. Type commands (`help`, line editing, arrows, backspace): echo and
      editing behave as on USB; output arrives once (no doubling).
   4. Paste a long line (~200 chars, under the 255 `MaxLine`) and a
      multi-line paste: nothing is lost or reordered.
   5. `ADDR=<UUID printed in step 2> make nus`: matching by address works.
   6. Ctrl-] quits and the terminal is restored (not stuck in raw mode).
   7. While connected, `nus off` on USB (or reset the board): nusterm prints
      `[disconnected]` and exits, terminal restored.
   8. Piped input: `echo help | make nus` prints help and exits after ~1 s.
   9. Optional: rough key-echo latency vs the Linux figure (27 ms median) to
      confirm the 15 ms interval took effect.
4. **ota, end to end**:
   1. Requires `adafruit-nrfutil` (`pip install adafruit-nrfutil`) for `make dfu`; check it is installed.
   2. `make ota`, then `ota` on the USB or BLE console. Expect it to find
      `AdaDFU`, print the ATT MTU, send the package and the board to boot the
      new firmware (banner shows the new commit/date).
   3. Optionally `ADDR=<AdaDFU UUID>`.
5. **Fix what fails**, keeping changes small and macOS-specific. Likely
   places: `src/nusterm/main.go`, `src/ota/main.go`, `src/internal/blescan/blescan.go`.
6. **README** (`README.md` BLE console and Over-the-air sections): record
   the results, e.g. ota prerequisites currently say "a Bluetooth adapter
   with BlueZ"; add macOS (UUID for `ADDR=`, Bluetooth permission) if ota
   works. Commit only when the user asks.

## Verification
The check passes when steps 1–4 all succeed on this Mac: tests green,
nusterm interactive/paste/address/quit/disconnect/pipe cases behave as on
Linux, and an OTA update from the Mac boots the new firmware. Report each
step's result, and any fix made with its reason.
