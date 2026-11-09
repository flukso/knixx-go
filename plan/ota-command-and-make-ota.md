# Over-the-air firmware update: `ota` command and `make ota`

> **Status: completed.** Implemented in commit `c570a52` ("update the
> firmware over BLE with make ota") (2026-10-19). Recovered from the
> planning session's transcript: the session reused one plan file for all
> its plans, so only the last one was left on disk.

## Context

We want to update the Feather's firmware over BLE. The Adafruit nRF52
bootloader already supports this. When it finds GPREGRET = 0xA8 at reset, it
starts its BLE DFU mode, advertising as `AdaDFU` and speaking Nordic's legacy
(SDK 11) DFU protocol.

The work has two halves:

1. **Device:** a new `ota` console command writes 0xA8 to GPREGRET and resets.
   It works from both consoles, USB CDC and the BLE NUS, since both run
   `cli.Exec` on the console goroutine.
2. **Host:** `make ota`, analogous to `make flash`.
   - It builds the DFU package (`build/knixx-dfu.zip`) only when the firmware
     sources changed.
   - It then waits for an `AdaDFU` advertiser and flashes the package over
     BLE.
   - `adafruit-nrfutil` can only *build* DFU packages (`dfu genpkg`), not send
     them over BLE. So the sending is done by our own Go host tool, using
     `tinygo.org/x/bluetooth` with BlueZ (user decision). The host has BlueZ
     running and an adapter (`hci0`).
   - The board is put into DFU mode by typing `ota` on a console; the tool
     does not trigger it (user decision).

## Device: `ota` command

- **Existing entry point.** `machine.EnterOTABootloader()`
  (`tinygo/root-0.42.0/src/machine/machine_nrf52840_enter_bootloader.go`)
  disables interrupts, writes `POWER.GPREGRET = 0xA8` and calls
  `arm.SystemReset()`. That is correct while the SoftDevice is off.
- **Once the SoftDevice is on** (after any `nus on` this boot, tracked by
  `bleEnabled` in `src/nrf/nus.go`), POWER is restricted, so GPREGRET must be
  written through the SoftDevice:
  - `sd_power_gpregret_clr(0, 0xff)` is SVC `0x2C+17`.
  - `sd_power_gpregret_set(0, 0xA8)` is SVC `0x2C+16`; it ORs bits in, which
    is why the register is cleared first.
  - Both numbers come from the vendored S140 6.1.1 `nrf_soc.h`.
  - Call them with `arm.SVCall2` while interrupts are still enabled: a
    SoftDevice SVC raised under PRIMASK escalates to a HardFault.
  - Then call `arm.SystemReset()`. `sd_nvic_SystemReset` is just
    `NVIC_SystemReset`.
- **New `src/nrf/ota.go`:**
  - `otaCmd()` takes no arguments.
  - It prints `ota: rebooting into the BLE DFU bootloader (AdaDFU); run make
    ota on the host`.
  - It sleeps 200 ms so the line reaches USB, or the phone over NUS.
  - Then it calls `enterOTA()`. On an SVC error, it reports with `fail` and
    does not reset.
  - The constants sit beside `sdClockHFCLKRequest`.
- Register `otaCmd()` in `src/nrf/main.go`.

## Host: legacy DFU protocol (SDK 11, as in the Adafruit bootloader)

**Characteristics** of the DFU service `00001530-1212-efde-1523-785feabcd123`:

| UUID | role |
|---|---|
| `…1531…` | control point (write, notify) |
| `…1532…` | packet (write without response) |

**Steps for an application update:**

1. Enable notifications on the control point.
2. Control point `01 04`: START, mode application.
3. Packet: 12 bytes, the softdevice, bootloader and application sizes as
   uint32 LE: `0, 0, len(bin)`.
4. Wait for the response `10 01 01`.
5. Control point `02 00`: init start. Packet: the `.dat` init packet. Control
   point `02 01`: init complete. Wait for `10 02 01`.
6. Control point `08 nn nn`: request a packet receipt notification every N
   packets (N = 10).
7. Control point `03`: receive the firmware.
8. Send the `.bin` in 20-byte writes; every write must be a multiple of 4
   bytes.
   - After every N packets, wait for `11` followed by the bytes received as
     uint32 LE, and check the count.
   - At the end, wait for `10 03 01`.
9. Control point `04`: validate. Wait for `10 04 01`.
10. Control point `05`: activate and reset. The link drops; that is expected.

**Status bytes** in a `10 op st` response: 1 success, 2 invalid state, 3 not
supported, 4 data size, 5 CRC error, 6 operation failed.

**Device address.** The bootloader advertises with its address incremented by
1, so the tool matches on the name `AdaDFU` (or on `-addr`), not on the
application's address.

## Changes

### 1. `src/internal/legacydfu/` (new, host-testable protocol package)

- **`Package`:**
  - `Open(zipPath)` reads `manifest.json` (`manifest.application.bin_file` and
    `dat_file`), as written by `adafruit-nrfutil dfu genpkg`, and returns
    `Package{Bin, Dat []byte}`.
  - It rejects a manifest without an application.
  - It rejects a `len(Bin)` that is not a multiple of 4, because the
    bootloader requires 4-byte multiples and the init packet's CRC forbids
    padding.
- **`Link` interface:** `WriteControl([]byte) error`, `WritePacket([]byte)
  error` and `Notifications() <-chan []byte`, so the protocol runs against a
  fake link in tests.
- **`Update(link, pkg, progress func(sent, total int)) error`:**
  - Runs the steps above.
  - Each wait has a timeout of 10 s, except validation at 30 s while the
    bootloader writes flash.
  - It decodes responses: a wrong opcode or a non-success status becomes an
    error naming the step and the status.
  - It checks the receipt counts.
- **`legacydfu_test.go`** uses a fake bootloader that answers like the real
  one. It checks:
  - the byte-exact control and packet sequence;
  - 20-byte chunking;
  - waits on receipt notifications;
  - error statuses, such as a CRC error at validation;
  - timeouts;
  - parsing of a zip built in memory, plus the size checks.

### 2. `src/ota/main.go` (new host command, BlueZ via `tinygo.org/x/bluetooth`)

- **Flags:** `-zip` (default `../build/knixx-dfu.zip`), `-name AdaDFU`,
  `-addr` (optional; when set, it overrides the name match), and `-wait 60s`.
- **Flow:**
  1. Enable `bluetooth.DefaultAdapter` and scan until an advertiser matches.
     Print `waiting for AdaDFU (run ota on the knixx console)…`.
  2. Connect and discover the DFU service and both characteristics.
  3. Adapt them to `legacydfu.Link`; notifications go into a buffered channel.
  4. Run `legacydfu.Update`, with a progress line in percent.
  5. Print `done, the board restarts with the new firmware`.
- **Exit status:** non-zero on any error, so that `make` fails.

### 3. `Makefile`

- **`HEX := build/knixx.hex` and `DFU := build/knixx-dfu.zip`.** They rebuild
  only when needed: `FW_SRC := $(shell find src -name '*.go' -o -name '*.ini'
  -o -name '*.bin') src/go.mod src/go.sum` plus `$(STAMP)`.
- **`$(HEX): $(STAMP) $(FW_SRC)`** runs `tinygo build -target=$(TARGET)
  -stack-size=$(STACK_SIZE) -o ../$(HEX) ./nrf`.
- **`$(DFU): $(HEX)`** runs `adafruit-nrfutil dfu genpkg --dev-type 0x0052
  --sd-req 0x00B6 --application $(HEX) $(DFU)`. 0x00B6 is the firmware ID of
  S140 6.1.1.
- **`dfu: $(DFU)`** builds the package only.
- **`ota: $(DFU)`** runs `cd src && go run ./ota -zip ../$(DFU) $(if
  $(ADDR),-addr $(ADDR))`.
- Add `dfu ota` to `.PHONY`, with a short comment like the existing ones.

### 4. `README.md`

- **Console table:** an `ota` row.
- **Building:**
  - `make dfu` and `make ota [ADDR=…]`.
  - `adafruit-nrfutil` is required.
  - A Bluetooth adapter with BlueZ is required for `make ota`.
- **New "Over-the-air update" section:**
  1. Run `make ota` on the host.
  2. Type `ota` on the USB console or the BLE console.
  3. The tool finds `AdaDFU`, sends the package, and the board restarts.

  Phone apps (nRF Connect's DFU, Bluefruit Connect) also work with
  `build/knixx-dfu.zip`.
- **Code layout:** add `ota` (host tool) and `internal/legacydfu`.
- **Caveats:**
  - `ota` resets the board: a telegram in flight is lost and the occupancy
    state is cleared.
  - The bootloader stays in DFU mode until updated or reset.
  - `make ota` picks the first `AdaDFU` it sees; use `ADDR=` when several
    boards are in DFU mode.

## Verification

1. `make test` passes, including the `legacydfu` tests; `go vet` covers
   `src/ota` on the host.
2. `make nrf` builds. `make dfu` builds the zip, and a second `make dfu` does
   nothing. Check the zip's contents with `unzip -l`.
3. On the board:
   1. Start `make ota`, then type `ota` on USB. The tool connects to `AdaDFU`,
      shows progress, validates and activates. The board boots the new
      firmware, and its boot banner shows on USB.
   2. Repeat with `ota` typed over BLE (`nus on` first). This exercises the
      SoftDevice GPREGRET path.
   3. Run `make ota` without the board in DFU mode: it times out after 60 s
      with a clear message.
   4. If the bootloader rejects the package because of `--sd-req`, rebuild it
      with `--sd-req 0xFFFE` and note which value works.
