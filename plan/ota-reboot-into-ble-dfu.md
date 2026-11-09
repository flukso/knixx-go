# `ota`: reboot into the bootloader's BLE DFU mode

> **Status: not implemented.** Superseded before implementation by
> `ota-command-and-make-ota.md`, after the user asked: "Can we have a
> makefile "ota" target analogous to the "make flash" one that triggers a
> dfu build (if necessary) and flashes it using a command line tool
> (adafruit-nrfutil?) over-the-air." (2026-10-18). Recovered from the
> planning session's transcript: the session reused one plan file for all
> its plans, so only the last one was left on disk.

## Context

We want to update the Feather's firmware over the air. The Adafruit nRF52
bootloader on the Feather already supports BLE DFU, and it enters that mode
when GPREGRET holds 0xA8 at reset. A new `ota` console command will write that
value and reset. It works from both consoles, USB CDC and the BLE NUS: both
run `cli.Exec` on the console goroutine, so the command needs no
transport-specific code.

## Findings

- **TinyGo already has the entry point.** `machine.EnterOTABootloader()` in
  `tinygo/root-0.42.0/src/machine/machine_nrf52840_enter_bootloader.go`
  disables interrupts, writes `nrf.POWER.GPREGRET = 0xA8` (`dfuMagicOTAReset`)
  and calls `arm.SystemReset()`.
- **With the SoftDevice enabled, POWER is restricted.** That is the case after
  any `nus on` in this boot. GPREGRET must then go through the SoftDevice:
  - `sd_power_gpregret_clr(0, 0xff)` is SVC `0x2C+17`, and
    `sd_power_gpregret_set(0, 0xA8)` is SVC `0x2C+16`, both from the vendored
    S140 6.1.1 `nrf_soc.h`. The set call ORs bits in, so clear first.
  - `sd_nvic_SystemReset` is only `NVIC_SystemReset`, so `arm.SystemReset()`
    is correct in both cases.
  - `runtime.isSoftDeviceEnabled` is not exported, but our `bleEnabled` flag
    in `src/nrf/nus.go` says the same thing.
- **The DFU package tool is installed.** `adafruit-nrfutil` is at
  `~/.local/bin`. BLE DFU through the bootloader takes a Nordic legacy DFU
  `.zip`, not a UF2.

## Changes

### 1. `src/nrf/ota.go` (new, TinyGo only)

- **Constants:** `dfuMagicOTA = 0xA8`, `sdPowerGPREGRETSet = 0x2C + 16` and
  `sdPowerGPREGRETClr = 0x2C + 17`, each with its header reference. They sit
  next to the existing `sdClockHFCLKRequest`.
- **`otaCmd() cli.Cmd`:** name `ota`, help "reboot into the bootloader for a
  BLE firmware update (DFU)", and no arguments; any argument prints `usage:
  ota`. It does the following:
  1. Prints `ota: rebooting into the BLE DFU bootloader; update with a DFU
     .zip, for example from the nRF Connect app`.
  2. Sleeps 200 ms so that the message reaches the terminal, over USB or as
     BLE notifications, before the link drops.
  3. Calls `enterOTA()`.
- **`enterOTA()`:**
  - If `bleEnabled`:
    1. Call `arm.SVCall2(sdPowerGPREGRETClr, 0, 0xff)`, then
       `arm.SVCall2(sdPowerGPREGRETSet, 0, dfuMagicOTA)`. Interrupts must stay
       enabled here, because a SoftDevice SVC raised while PRIMASK masks
       interrupts escalates to a HardFault.
    2. If either call returns an error, report it with `fail` and don't reset.
    3. Otherwise call `arm.SystemReset()`.
  - Otherwise: `machine.EnterOTABootloader()`.
- Register `otaCmd()` in `src/nrf/main.go` next to `nusCmd()`.

### 2. `Makefile`: target `dfu`

- `make dfu` builds `build/knixx.hex` with the same flags as `nrf`.
- It then runs `adafruit-nrfutil dfu genpkg --dev-type 0x0052 --sd-req 0x00B6
  --application build/knixx.hex build/knixx-dfu.zip`. 0x00B6 is the firmware
  ID of S140 6.1.1.
- Add a comment that this builds the package that `ota` expects, and add `dfu`
  to `.PHONY`.

### 3. `README.md`

- **Console table:** add an `ota` row.
- **Building:** add `make dfu`, which builds `build/knixx-dfu.zip` and needs
  `adafruit-nrfutil`.
- **"Firmware update over the air" section:**
  1. Run `ota` on either console.
  2. The board resets into the bootloader, which advertises as `AdaDFU`.
  3. Send `build/knixx-dfu.zip` with the nRF Connect or nRF Device Firmware
     Update app, or Adafruit Bluefruit Connect.
  4. The bootloader then starts the new firmware.
- **Caveats:**
  - A reset ends any telegram in flight and clears the occupancy state.
  - While in DFU mode the device does nothing else.

## Verification

1. `make test` passes, and `make nrf` and `make dfu` build. Check that the
   `.zip` holds the `.hex` and a `.dat`.
2. On the board:
   1. Run `ota` on USB. The message shows and the USB serial port disappears.
      A phone sees `AdaDFU`, and nRF Connect's DFU sends `knixx-dfu.zip`. The
      board then boots the new firmware, which the boot banner confirms.
   2. Run `nus on`, connect, and run `ota` over BLE. The message reaches the
      phone and the board enters DFU, which checks the SoftDevice GPREGRET
      path.
   3. Unknown: does the bootloader leave DFU mode by itself after a timeout
      without an update? Note what happens. If it doesn't, a reset or power
      cycle gets back to the application.
   4. If the bootloader rejects the package because of `--sd-req`, retry with
      `--sd-req 0xFFFE` (any SoftDevice) and note which one works.
