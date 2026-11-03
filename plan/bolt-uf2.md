# bolt uf2: USB flashing in our own host tool

> **Status: completed.** Implemented in commit `551bf65` ("flash over USB
> with bolt uf2 instead of tinygo flash"), followed by `e4a11c9`
> (2026-10-21). Recovered from the planning session's transcript: the
> session reused one plan file for all its plans, so only the last one was
> left on disk.

## Context
`make flash` runs `tinygo flash`, which on macOS reports an I/O error on every
flash that works: the UF2 bootloader resets the board as soon as it has the last
block, while macOS still writes the drive's file entries. We patched around it
with `tools/flashcheck.sh` (332242b). Moving the UF2 procedure into `bolt` as
`bolt uf2` puts the whole flow under our control: we expect the reset, verify
the result ourselves, and check the file before touching the board. `make flash`
then builds with TinyGo and flashes with `build/bolt uf2`.

## TinyGo's procedure (v0.43.0 main.go), carried over
Target `feather-nrf52840`: `flash-1200-bps-reset`, `flash-method: msd`,
`msd-volume-name: FTHR840BOOT`, `uf2-family-id: 0xADA52840`,
`serial-port: 239a:8029 (bootloader), 239a:802a (app)`.
1. Find the app's serial port (`getDefaultPort`: USB VID:PID match, `-port`).
2. `touchSerialPortAt1200bps`: open at 1200 baud (up to 3 tries, 1 s apart),
   drop DTR, close: the board resets into the UF2 bootloader.
3. Sleep 3 s, then poll every 500 ms until a timeout for a FAT mount named
   `FTHR840BOOT` that has `INFO_UF2.TXT` (darwin: `/Volumes`; linux:
   `/proc/mounts`, vfat).
4. Copy the .uf2 to `<mount>/flash.uf2` (O_TRUNC).

## Design (serial via golang.org/x/sys/unix only, no new dependency)

**`src/host/internal/uf2`** (new)
- `Check(data []byte, family uint32) error`: 512-byte blocks, both start
  magics and the end magic, the family-ID flag set with `family`, block numbers
  0..n-1 with one `numBlocks`. Rejects a wrong or truncated file before the
  board is reset.
- `FindVolume(names []string) (string, bool)`: darwin `/Volumes/<name>`,
  linux mounts from `/proc/mounts` (vfat) whose last path element is the name;
  either must hold `INFO_UF2.TXT`. The `/proc/mounts` parsing is a function of
  an `io.Reader` so it is testable. `BoardID(mount)` returns the `Board-ID:`
  line of `INFO_UF2.TXT`, for the log.

**`src/host/internal/usbserial`** (new)
- `Ports(vidpid ...string) ([]string, error)`: linux, `/sys/class/tty/ttyACM*`
  whose USB device (`device/../idVendor`, `idProduct`) matches; darwin,
  `/dev/cu.usbmodem*` (no USB IDs without IOKit; `-port` picks among several).
  The sysfs walk takes a root directory, for a test with a fake tree.
- `Reset1200(port string) error`: open (O_NOCTTY|O_NONBLOCK), set raw 1200
  baud with termios (`TIOCGETA/TIOCSETA` on darwin, `TCGETS/TCSETS` on linux,
  in `termios_darwin.go` / `termios_linux.go`), clear DTR (`TIOCMBIC`), close;
  3 tries, 1 s apart, as TinyGo.
- `Query(port, cmd string, wait time.Duration) (string, error)`: open raw at
  115200, send `\r` then `cmd\r`, collect output for `wait` with a reader
  goroutine (the fd is closed to end it), return it with ANSI escapes and CRs
  removed. Used for `version` after the flash.

**`src/host/bolt/uf2.go`** (new) — `runUF2(file, port, volume, expect, wait)`:
1. Read the file and `uf2.Check(data, 0xADA52840)`.
2. If the volume is mounted already (board in the bootloader, e.g. after a
   double-tap), skip the reset. Else find the app port (`usbserial.Ports
   ("239a:802a")` or `-port`; error on none or several) and `Reset1200` it;
   wait 3 s.
3. Poll `uf2.FindVolume` every 500 ms up to `-wait`; log the Board-ID.
4. Write `<mount>/flash.uf2`. An error is expected (the reset), so it is
   logged and left to step 5.
5. With `-expect`: poll for the app port and `Query(port, "version")` until
   `-wait`; pass only if a line equals `-expect`. Without it: pass once the
   volume is gone (the board left the bootloader); fail if the write failed
   and the volume stays.

**`src/host/bolt/main.go`**: add `uf2` to the dispatch and usage (`uf2Cmd`
with a FlagSet: `-port`, `-volume FTHR840BOOT`, `-expect`, `-wait 20s`, file
argument, default `build/knixx.uf2`); doc comment: BLE and USB.

**`Makefile`**
- `flash: nrf bolt` → `build/bolt uf2 -expect "version $(GIT_COMMIT) built
  $(BUILD_DATE)" $(if $(PORT),-port $(PORT)) $(FIRMWARE)` (same make run, so
  the same BUILD_DATE as the `nrf` build); comment updated.
- Delete `tools/flashcheck.sh`.

**`README.md`**: `make flash` line (PORT=), replace the "Flashing on macOS"
note with the bolt uf2 flow (and on Linux: the drive must be automounted, the
user needs access to /dev/ttyACM*), bolt row in the layout table, the two new
internal packages, `host/` wording (BLE and USB host tools).

## Verification
- `go test ./host/...`: uf2.Check (valid file from `make nrf`, wrong family,
  truncated, bad magic, gap in block numbers), /proc/mounts parsing, sysfs
  port matching with a fake tree. `make test` passes (allocs unchanged: host
  code is not in the firmware).
- Hardware (stop the soak first, restart it after):
  - `make flash` from the running app: reset, volume, copy, version matches,
    exit 0 — no I/O error shown as a failure.
  - Board already in the bootloader (double-tap): `make flash` skips the reset
    and flashes.
  - `build/bolt uf2 -expect "version x built y" build/knixx.uf2` exits 1 with
    the version the board reports.
  - A UF2 with another family ID is refused before the board is reset.
- Linux can't be tested here; the sysfs and /proc/mounts paths are covered by
  the unit tests.
- Commit only when asked.
