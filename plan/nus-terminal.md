# NUS terminal: the BLE console from a Linux or macOS terminal

> **Status: completed.** Implemented in commit `a8488d1` ("add a terminal
> for the BLE console") (2026-10-20). Recovered from the planning session's
> transcript: the session reused one plan file for all its plans, so only
> the last one was left on disk.

## Context

Today the BLE console (`nus on`) is reached through phone apps such as nRF
Connect or Serial Bluetooth Terminal. We want to connect from a laptop
terminal through the computer's own Bluetooth chipset, preferably with one Go
program that runs on Linux and macOS.

## Analysis

- **Library: `tinygo.org/x/bluetooth` v0.16.0, already a dependency.** The
  host tool `src/ota` uses it.
  - It has a central-role API with the same calls on each platform: `Scan`,
    `Connect`, `DiscoverServices`, `DiscoverCharacteristics`,
    `EnableNotifications`, `Write` and `WriteWithoutResponse`.
  - It runs on Linux through BlueZ over D-Bus, on macOS through CoreBluetooth
    (cbgo) and on Windows through WinRT.
  - So a NUS terminal is a small central, much like `ota`.
- **The firmware side needs no change:**
  - The board advertises as `knixx` with the NUS service UUID.
  - RX is writable with or without response, and TX notifies
    (`src/nrf/nus.go`).
  - Output arrives as notifications of up to 20 bytes, already with CRLF line
    endings, so a raw-mode terminal shows it as it is.
  - The firmware shows the banner and prompt on the first key it receives
    (`greeted` in `src/nrf/console.go`). The tool sends one CR after
    subscribing so that the prompt appears at once.
  - Input goes into a 256-byte ring (`nus.Ring`). Writing each chunk with
    response (`Write`) gives natural backpressure: one chunk per connection
    event. Pastes then cannot overflow the ring, and typing speed is
    unaffected.
- **Terminal handling:**
  - `golang.org/x/term` (new, pure Go, cross-platform) puts stdin into raw
    mode. Keys, including Ctrl-C, backspace and arrows, then go to the board's
    line editor as they would over USB.
  - **Ctrl-]** quits, as in telnet. The terminal is restored on every exit
    path.
- **Platform caveats:**
  - **Linux:** needs `bluetoothd` running. No root is needed, since D-Bus
    access is the default for desktop users. BlueZ exchanges the MTU itself.
  - **macOS:**
    - **Addresses:** CoreBluetooth hides MAC addresses. Each device gets a
      per-host UUID, so `-addr` takes that UUID on macOS. Matching by name,
      the default, works the same on both.
    - **Permission:** the first run triggers the macOS Bluetooth permission
      prompt for the terminal app (Terminal or iTerm).
    - **Build:** cbgo needs cgo, so the tool is built with `go run`/`go build`
      on the Mac itself. Cross-compiling from Linux isn't practical.
  - **Windows:** expected to work through WinRT and x/term, but untested.
- **One central at a time.** If a phone holds the connection, the board
  doesn't advertise, and the scan times out with a clear message.

## Plan

1. **Shared scanning.** Move `scan()` from `src/ota/main.go` into a new
   `src/internal/blescan` package, as `blescan.Find(adapter, name, addr,
   wait)`. Both tools use it. The behaviour is unchanged: match by
   `LocalName`, or by address or UUID when `-addr` is set.
2. **New command `src/nusterm/main.go`**, a host-only Go program:
   - **Flags:** `-name knixx`, `-addr` (MAC on Linux, UUID on macOS) and
     `-wait 30s`.
   - **Setup:** enable the adapter, `blescan.Find`, `Connect`, then discover
     the NUS service and its RX and TX characteristics (UUIDs from
     `bluetooth.ServiceUUIDNordicUART` and the related constants).
   - **Output:** subscribe to TX and copy each notification to stdout.
   - **Prompt:** send `\r`, which brings up the banner and prompt.
   - **Input:** stdin in raw mode, read in a goroutine. Ctrl-] (0x1d) ends the
     session; everything else is written to RX in chunks of `MTU−3` bytes (20
     by default) with `Write`.
   - **Disconnect:** watch `adapter.SetConnectHandler`. A disconnect, for
     example from `nus off`, the idle timeout or a reset, prints
     `[disconnected]`, restores the terminal and exits with 0.
   - **Status messages:** `connecting to …`, `connected, Ctrl-] to quit`, and
     so on go to stderr on their own lines, before raw mode starts and after
     it ends.
3. **Testable pieces in `src/nusterm/term.go`:** an `input` splitter that
   takes a read from stdin and returns the chunks to send, stopping at the
   escape byte. Tests in `term_test.go` cover:
   - chunks at the MTU limit;
   - escape in the middle of a read;
   - escape alone;
   - an empty read.
4. **Dependencies:** `go get golang.org/x/term` in `src/`.
5. **Makefile:** a `nus` target, `cd src && go run ./nusterm $(if
   $(ADDR),-addr $(ADDR))`, in the style of `ota`.
6. **README:**
   - in the BLE console bullet: `make nus`, or `go run ./nusterm` from `src/`,
     connects from Linux or macOS, and Ctrl-] quits;
   - a row for `nusterm` and `internal/blescan` in the code layout table;
   - the macOS notes: per-host UUID, Bluetooth permission and building on the
     Mac.

## Verification

- `make test`: vet plus the new splitter tests, and `ota` still builds after
  the scan move.
- **On Linux with the board:**
  - `nus on` on USB, then `make nus`. The banner and prompt appear.
  - Run `version`, `sniff on` with bus traffic, and paste a long line.
  - Ctrl-] quits and leaves a sane terminal.
  - Typing `nus off` gives `[disconnected]` and the console is back on USB.
  - Waiting out a disconnect: reconnecting within 60 s works, and after the
    idle timeout the scan reports "not found".
- **On macOS:** you run `cd src && go run ./nusterm` on a Mac. I can't test
  that here.
- **`make ota`:** still finds the bootloader through the shared scan.
