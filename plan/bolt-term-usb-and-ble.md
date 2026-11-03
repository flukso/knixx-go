# bolt term over USB and BLE

> **Status: completed.** Implemented in commit `3994a0d` ("bolt term on the
> USB console too, with make term") (2026-10-21). Recovered from the
> planning session's transcript: the session reused one plan file for all
> its plans, so only the last one was left on disk.

## Context
`bolt term` is a terminal for the BLE console only (NUS, `make nus`). The USB
CDC console needs `screen`, which on macOS fights other users of the port
(the tty./cu. lock), garbles nothing but quits awkwardly, and differs from
the BLE terminal (Ctrl-] vs Ctrl-A k). One terminal for both: `bolt term
[-usb|-ble]`; with neither, USB if the board answers there, else BLE.

## What the firmware does (src/nrf/console.go, nus.go)
- The console runs on one port at a time (`active`). After `nus on`, USB
  input is ignored and the USB port gets no output; `nus off` (or the 60 s
  idle timeout, or a reset) moves it back to USB.
- The banner and prompt show on the first key; bolt already sends a `\r`
  first on BLE. On USB a `\r` always gets an answer while the console is on
  USB: the banner and prompt, or a fresh prompt.
- So "USB available" = a knixx app port exists (`usbserial.Ports("239a:802a")`,
  as `bolt uf2`'s `appPort`) **and** it answers a `\r`. A port that stays
  silent means the console is on BLE, which is exactly when BLE is the right
  fallback. Without a USB port at all (board powered from the KNX bus only,
  or on another computer) BLE is the only way.

## Design

**`src/host/internal/usbserial/usbserial.go`**: export the open port:
`type Port` (today's unexported `port`), `Open(name string, baud int)
(*Port, error)`, `Read`, `Write`, `SetReadDeadline`, `Close`. `Reset1200` and
`Query` use it unchanged in behaviour.

**`src/host/bolt/term.go`**: split the transport from the session.
- `conn` interface: `write(p []byte) error`, `chunk() int` (write size),
  `gone() <-chan struct{}` (closed when the link drops), `close()`. Received
  bytes go straight to stdout from the conn's own reader.
- `session(c conn, in io.Reader, greet bool) error`: today's loop of
  `runTerm` — raw mode when `in` is a terminal, the first `\r` when `greet`,
  `pending`/`chunks` from `input.go` (unchanged), Ctrl-] quits, EOF waits 1 s,
  `[disconnected]` when `gone` closes.
- `bleConn`: today's find/connect/openNUS code, writes with response in MTU-3
  chunks, `gone` from the connect handler.
- `usbConn`: a `usbserial.Port` at 115200; a goroutine copies reads to stdout
  and closes `gone` on a read error (unplug, reset by `ota` or a flash); writes
  of up to 256 bytes (USB flow control paces them, no response to wait for).
- `runTerm(mode, port, name, addr string, wait time.Duration)`:
  - `usb`: `-port`, else the one app port (error on none or several, as
    `appPort` in `uf2.go`, which moves to a shared helper); greet.
  - `ble`: as today.
  - auto: app port found → open it, send `\r`, read up to 500 ms. Output →
    print it and run the session (no second greet). Silence → close, report
    "no answer on USB (the console is on BLE?), trying BLE", BLE. No app port
    → report and BLE. An open error (macOS: a `screen` holds the port) is
    returned, not a fallback: scanning BLE for 30 s would hide the cause.

**`src/host/bolt/main.go`**: `termCmd` flags `-usb`, `-ble` (exclusive),
`-port` (implies `-usb`), `-name`/`-addr`/`-wait` (BLE; `-addr` implies
`-ble`). Usage and doc comment: "a terminal on the board's console, over
USB or BLE".

**`Makefile`**: new `term: bolt` → `build/bolt term $(if $(PORT),-port
$(PORT)) $(if $(ADDR),-addr $(ADDR))`; `nus` → `build/bolt term -ble …`
(unchanged behaviour). Add `term` to `.PHONY`.

**`README.md`**: the console section uses `make term`; `screen`, the
`tinygo monitor` remark and the tty./cu. note go (bolt reports a busy port
itself); every other `screen` mention goes too; `make term` in the
command list; the BLE console section's `make nus` stays; layout table row of
`host/bolt` and `host/internal/usbserial`.

Out of scope: following the console when `nus on`/`nus off` is typed during a
session (the USB terminal then goes silent; quit and rerun `make term`).

## Verification
- Unit tests (`go test ./host/...`):
  - `session` with a fake conn and an `io.Reader` input: keys are sent,
    Ctrl-] quits and sends the bytes before it, EOF ends after the wait,
    `gone` ends with `[disconnected]`, greet sends one `\r`.
  - flag combinations: `-usb -ble` and `-port -ble` are errors; `-addr`
    selects BLE.
  - existing `chunks`/`pending` and usbserial tests still pass.
- `make test` (host only, allocs unchanged).
- Hardware (soak stays off):
  - `make term` on the Mac: banner, `sys`, history keys, a pasted line,
    Ctrl-] quits and the shell is restored.
  - `echo version | build/bolt term -usb` prints the version and exits.
  - `nus on` on the USB console, quit, `make term`: no USB answer → falls back
    to BLE and connects; `nus off` there.
  - USB unplugged: `make term` goes to BLE (with `nus on` via the switch
    gesture). `bolt term -usb` errors with no port.
  - Unplug during a USB session → `[disconnected]`.
  - `screen` holding the port → `make term` reports the busy port.
- Commit only when asked.
