# BLE console over the Nordic UART Service (NUS)

> **Status: completed.** Implemented in commit `69a61a6` ("add a BLE console
> on the Nordic UART Service") (2026-10-18). Recovered from the planning
> session's transcript: the session reused one plan file for all its plans,
> so only the last one was left on disk.

## Context

Today the console only runs on USB CDC. We want to take the cli over BLE:

- **`nus on`**, typed on USB, enables BLE, advertises the Nordic UART Service
  and attaches the cli to it.
- **`nus off`**, typed over BLE, stops BLE and attaches the cli back to USB.

**Decisions (user):**
- **`nus off`:** drops the connection and stops advertising, so the radio goes
  silent. The SoftDevice stays enabled and idle until the next reset, because
  tinygo `bluetooth` has no disable.
- **Security:** none (no pairing). Only one central at a time.
- **Escape:**
  - A reset brings the cli back to USB, since NUS is never on at boot.
  - A 300 s timer runs whenever NUS is on with no central connected. It starts
    at `nus on` and restarts on every disconnect. When it expires, NUS turns
    off as if `nus off` had been typed.

## Findings

- **SoftDevice already in the build.** The build already links against the
  SoftDevice layout.
  - `feather-nrf52840` inherits `nrf52840-s140v6-uf2`, with the build tags
    `softdevice` and `s140v6`.
  - The app starts at 0x26000 and RAM at 0x20004180, where `__app_ram_base` is
    set.
  - The Adafruit bootloader ships S140 v6.1.1.
  - Nothing new is needed for the linker, flash or RAM layout; we only add
    `tinygo.org/x/bluetooth`.
- **What tinygo `bluetooth` provides** (`adapter_sd.go`):
  - `DefaultAdapter.Enable()`, but no Disable.
  - BLE events are handled in the **SWI2 interrupt** at priority 192. A
    characteristic's `WriteEvent` callback, which is our NUS RX, runs in
    interrupt context: it must not allocate, block or print.
  - The package has the NUS UUIDs (`ServiceUUIDNordicUART`,
    `CharacteristicUUIDUARTRX`/`TX`) and advertising (`DefaultAdvertisement`,
    `Configure`, `Start`, `Stop`).
- **Interrupt priorities are compatible with the SoftDevice.** It reserves
  priorities 0, 1 and 4. TinyGo uses 2 for USBD and GPIOTE
  (`machine_nrf52840_usb.go:89`, `machine_nrf.go:157`) and 6 for RTC1 and
  UART.
  - NVMC belongs to the SoftDevice once enabled, but littlefs is on the
    external SPI flash, so it is unaffected.
- **Console code today:**
  - `nrf/console.go` hard-wires `machine.Serial`: `runConsole` polls
    `Buffered`/`ReadByte`, `lineedit.Editor.Out` is `machine.Serial`, and
    `crlf` writes to `machine.Serial`.
  - `console` is also `bus.Out` and the output of `fail`.

## Risks to check on hardware

1. **USB needs the HFXO.** TinyGo never starts the HFXO itself; the bootloader
   leaves it running. Once enabled, the SoftDevice owns CLOCK and may stop the
   HFXO, which would kill USB CDC.
   - Mitigation: right after `Enable`, call `sd_clock_hfclk_request` through
     `arm.SVCall0`, in the style of `runtime_nrf_softdevice.go`.
   - Take the SVC number from the `nrf_soc.h` vendored in the bluetooth
     module; the SOC SVCs start at 0x2C.
   - It costs nothing extra, because the HFXO already runs today.
2. **LFCLK reconfiguration.** Enabling the SoftDevice mid-run reconfigures the
   LFCLK, so RTC1, TinyGo's clock, may hiccup briefly. Check that the sensor
   ticks and the occupancy timeout keep their pace.
3. **Global interrupt disables.** `ws2812` disables interrupts for about 10 µs
   per byte. The SoftDevice usually tolerates this, but watch for SoftDevice
   asserts (a hard fault or reset) while the LEDs play during a connection.
4. **Flash size.** The bluetooth package adds roughly 20–40 KB; we are at 246
   KB of the 812 KB available.

## Design

### 1. New host-testable package `src/nus`

This package is pure logic, with no `machine` or `bluetooth` imports.

- **`Ring`:** a fixed `[256]byte` single-producer/single-consumer ring.
  - `Put(p []byte) int` is called from the SWI2 write handler. It doesn't
    allocate, and it drops what doesn't fit.
  - `Buffered() int` and `ReadByte() (byte, error)` are called from the
    console goroutine.
  - Head and tail are `atomic.Uint32`.
- **`Writer{Send func([]byte) error; Connected func() bool}`:**
  - `Write` splits the output into 20-byte notifications, the payload of the
    default ATT MTU.
  - On a send error such as no free TX buffers, it retries with `time.Sleep(5
    ms)` for up to 1 s per chunk, then drops the rest.
  - When no central is connected it drops everything. Writers must never block
    forever, because `bus.Run` and the sensors print to the console.
- **`Timer`:**
  - `Arm(now)` and `Disarm()`.
  - `Expired(now) bool` with `const IdleTimeout = 300 * time.Second`.
  - It holds the idle-timeout logic so that it can be tested.
- **Tests** cover:
  - ring wrap-around, overflow and reads;
  - chunking at 20 bytes;
  - retry and give-up;
  - dropping while disconnected;
  - the timer's arm, disarm and expiry.

### 2. A pluggable console port in `src/nrf/console.go`

- **`port` interface:** `interface{ Buffered() int; ReadByte() (byte, error);
  Write([]byte) (int, error) }`. `machine.Serial` already satisfies it.
- **Active port:** `var active port = machine.Serial`, plus `attach(p port,
  banner bool)`. Only the console goroutine calls `attach`, so it is safe on
  the cooperative scheduler.
  - `crlf.Write` writes to `active`, which redirects `bus.Out` and `fail` too.
  - `lineedit.Editor.Out` becomes a small `activeOut` writer, so echo follows
    the port.
  - `runConsole` polls `active`.
- **Switching:**
  - On a switch it resets the line editor and prints the banner and prompt on
    the new port.
  - USB input is not read while NUS is active, so any USB input waits in the
    CDC buffer.
  - On going back to USB, that stale buffered input is discarded and the
    prompt is reprinted.

### 3. `src/nrf/nus.go` (new, TinyGo only)

- **`nusOn()`:**
  1. The first time only: run `bluetooth.DefaultAdapter.Enable()` and the
     HFCLK request.
  2. Set the device name to `knixx <individual>`, for example `knixx 7.8.1`.
  3. Add the NUS service:
     - **RX** (write and write-without-response): its `WriteEvent` is
       `ring.Put`.
     - **TX** (notify).
  4. Set a connect handler that records `connected` (an `atomic.Bool`) and the
     `bluetooth.Device` so that `nusOff` can disconnect.
  5. Configure advertising with the NUS UUID and the name, and start it.
  6. Arm the idle timer and attach the console to the NUS port.
  - On any later `nus on`, only advertising restarts.
- **Connect and disconnect:** both happen in interrupt context. The handler
  only sets `connected` and stores the device.
  - The console goroutine sees the edges when it polls.
  - On connect it disarms the timer and prints the banner and prompt to the
    new central.
  - On disconnect it re-arms the timer. The SoftDevice restarts advertising by
    itself on disconnect.
- **`nusOff()`:** stop advertising, disconnect the central if one is
  connected, and attach the console back to USB. The "back on USB" message
  goes only to USB.
- **Idle timeout:** `runConsole` checks the timer on each 10 ms poll while NUS
  is active; on expiry it calls `nusOff()` and notes it on USB.
- **`nusCmd()`:** `cli.Cmd` named `nus`, with the usage `nus on|off`.
  - `on` works when the console is on USB; on NUS it reports "already on".
  - `off` works when the console is on NUS; on USB it reports "not on".
  - `nus` alone reports the state: off, advertising, or connected with the
    time left on the idle timer.
- **Registration:** `nusCmd()` is added in `main.go` next to `sensorCmd()` and
  `tofCmd(dev)`. Setting up BLE takes no extra goroutine: everything runs from
  the console goroutine and the SWI2 handler.

### 4. Dependency

`cd src && go get tinygo.org/x/bluetooth@<latest release>`. The package is
imported only under the `tinygo` build tag in `nrf`, so the host build
(`ftdi`) and the host tests are unaffected.

### 5. `README.md`

- Functionality: a BLE console bullet.
- The `nus` row in the console command table.
- A `nus` entry in the code layout table.
- Caveats:
  - no pairing;
  - the 300 s idle timeout;
  - reset as the escape;
  - the SoftDevice stays enabled after `nus off`;
  - typing in the BLE terminal app sends whole lines, which the editor
    handles.

## Verification

1. `make test` passes, including the new `nus` package tests. `make nrf`
   builds; note the flash and RAM size.
2. On the board, over USB with `screen`:
   1. `nus on`: USB says the console moved to BLE.
   2. nRF Connect or "Serial Bluetooth Terminal" on a phone shows `knixx
      7.8.1`.
   3. After connecting, the banner and prompt appear; `help`, `sensor` and
      `config dump` work. `config dump` checks chunking over multiple
      notifications.
   4. `nus off` over BLE disconnects the phone, advertising stops, and the
      prompt is back on USB.
   5. `nus on` again, without connecting: after 300 s the console is back on
      USB.
   6. Throughout: USB CDC stays enumerated (the HFXO check), the LEDs keep
      working with no SoftDevice assert, and KNX telegrams and occupancy keep
      going out (`sniff on`).
   7. Reset while on NUS: the device boots with the console on USB and BLE
      off.
