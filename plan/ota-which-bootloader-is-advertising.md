# OTA: telling which board's bootloader is advertising

> **Status: not implemented.** Research only: the stock bootloader cannot
> take a name from the app; the user chose "Just the information", so no
> change was made (2026-10-20). Recovered from the planning session's
> transcript: the session reused one plan file for all its plans, so only
> the last one was left on disk.

## Context

The user asked whether the app can make the Adafruit bootloader (0.11.0, S140
6.1.1) advertise a string the app chooses, right before `ota` resets into BLE
DFU. The aim is presumably to recognise the right board when several advertise
as `AdaDFU`.

## Findings (Adafruit_nRF52_Bootloader 0.11.0 source)

- **The name is fixed at build time.**
  - `lib/sdk11/.../dfu_transport_ble.c` defines `#define DEVICE_NAME "AdaDFU"`
    inside `#ifndef DEVICE_NAME`, limited to 8 characters.
  - `gap_params_init()` and `advertising_init()` use it as the GAP name and
    the complete local name.
  - The Device Information Service strings are compile-time macros too.
- **The app can't hand over any string.**
  - `check_dfu_mode()` in `src/main.c` only reads GPREGRET, for the magics
    0xA8, 0xB1, 0x4E, 0x57 and 0x6D.
  - It also reads the single-tap flag in flash and the double-reset word at
    0x20007F7C.
  - Neither UICR nor any no-init RAM is read for a name.
- **Peer data carries no name.** The SVC `dfu_ble_svc_peer_data_set` on the
  0xB1 jump path takes bond data only: address, IRK, encryption keys and
  CCCDs.
- **The bootloader's address is predictable.** Without peer data, which is our
  0xA8 reset path, `dfu_transport_ble_update_start()` does
  `sd_ble_gap_addr_get` and then `addr.addr[0] += 1`. So the bootloader
  advertises at the app's BLE address with the least significant octet + 1
  (mod 256), the last octet in the `XX:…:YY` notation. TinyGo's bluetooth
  keeps the SoftDevice default (FICR) address, so this holds for us.

So the stock bootloader can't do what was asked. The workable options are:

1. **Identify by address, recommended, with no bootloader change.**
   - The `ota` command prints the address the bootloader will use, e.g. `ota:
     rebooting into the BLE DFU bootloader (AdaDFU at F1:2C:…:A5)`, read with
     `sd_ble_gap_addr_get` before the reset.
   - The same address can also be shown in the banner or in `version`.
   - `make ota ADDR=…` already targets one board (`-addr` in
     `src/ota/main.go`).
   - Optionally, `src/ota` gets `-app <app address>` and adds 1 to the last
     octet itself, so the address printed by `nus`/BLE scanners for the
     running app can be used directly.
2. **A rebuilt bootloader with a different fixed name.** Build it with
   `DEVICE_NAME=...` and install it through its UF2 self-update image. The
   name is still the same on every board, so it doesn't help to tell boards
   apart.
3. **A patched bootloader that takes a name from the app.**
   - The app writes a magic word and up to 8 characters into a fixed RAM word
     next to `DFU_DBL_RESET_MEM` before resetting with 0xA8, and the patched
     `advertising_init()` uses them.
   - That means a maintained bootloader fork and a riskier bootloader update
     on every board, and we'd have to check that the area survives the
     bootloader's startup.

## Recommended change, if wanted (option 1)

- **`src/nrf/ota.go`.** Before the GPREGRET write and reset, read the GAP
  address with `sd_ble_gap_addr_get`. TinyGo's bluetooth `Adapter.Address()`
  works if the SoftDevice is enabled; if it isn't, enable it, or read FICR
  DEVICEADDR directly, since the SoftDevice derives its default address from
  FICR. Add 1 to octet 0 and print the result in the `ota` message.
- **`src/ota/main.go`.** Add an optional `-app` flag that derives the
  bootloader address the same way. `ADDR=` keeps working.
- **README.** In the OTA section: the bootloader advertises as `AdaDFU` at the
  app address + 1, and the `ota` command prints it.

## Verification

- Run `ota` on the console and check that the printed address matches the
  `AdaDFU` advertiser in `bluetoothctl scan on`.
- Run `make ota ADDR=<printed>` and check the update goes through.
