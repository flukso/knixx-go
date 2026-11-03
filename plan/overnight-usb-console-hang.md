# Find the cause of the overnight USB console hang

> **Status: completed.** Implemented in commit `52ccc43` ("add the sys
> command to diagnose a stuck console") (2026-10-21). Recovered from the
> planning session's transcript: the session reused one plan file for all
> its plans, so only the last one was left on disk.

## Context
Twice the USB console stopped answering after the board ran overnight;
a reset fixed it. While stuck, the board still enumerated as the app
(USB PID 0x802a) and `/dev/cu.usbmodem1101` existed, but nothing came back,
and a `nus on` typed on USB did not start BLE advertising. Two candidates:

1. **USB stack only.** TinyGo 0.43's nRF52840 USB IRQ handler
   (`machine_nrf52840_usb.go:200`) handles only READY, not bus reset,
   suspend or resume; the CDC TX pump (`machine/usb/cdc/usbcdc.go`) keeps
   `txActive` = 1 until a TX-complete IRQ, so an IN packet lost to a reset
   stalls USB output forever, and a busy EasyDMA owner makes the IRQ
   handler return early. The Mac sleeps every 15-20 min overnight
   (`pmset -g log`). A single sleep/wake with an idle console did not
   reproduce it, which fits a race (a packet must be in flight), but
   weakens the case.
2. **Whole firmware stuck**, e.g. heap exhaustion or a goroutine stuck
   after hours of uptime.

Goal: the next hang tells us which, and with the numbers to pin it down.
No fix is attempted until then.

## 1. A `sys` console command (firmware)
New file `src/nrf/sys.go`, registered in `runConsole`'s command list in
`src/nrf/main.go` next to `versionCmd()`, in the style of `nrf/version.go`
(`cli.Cmd`, usage line on extra arguments). It prints:
- `uptime` from `time.Since(boot)` (boot set at the start of `main`);
- heap from `runtime.ReadMemStats`: `HeapInuse`, `HeapSys` (or the fields
  TinyGo's `gc_blocks.go` fills), `Mallocs - Frees`, `NumGC`;
- `runtime.NumGoroutine()`;
- per console port (`usbOut`, `nusOutQ` in `nrf/console.go` / `nrf/nus.go`):
  bytes buffered and bytes dropped. `outq.Queue` gets a read-only
  `Lost() uint32` (total dropped, never cleared) next to `Dropped()` in
  `src/outq/outq.go`, with a test in `outq_test.go`, because `Dropped()`
  is cleared by the sender.
- `usb dtr` from `machine.Serial.DTR()`.

A stuck USB sender shows as `usb` buffered at the queue size (2048) and
`lost` growing between two `sys` calls; a heap problem as `HeapInuse`
near `HeapSys` or rising over hours. README: one row in the command table.

## 2. Overnight soak log (host, not committed)
A script in the session scratchpad polls the board every 5 min over USB:
open the port, send `\r` and `sys`, read 2 s, close, log with a host
timestamp to a file in the scratchpad. Opening and closing each time keeps
the port closed between polls, like the nights the hang happened. It runs
under `nohup`, without `caffeinate`, so that the Mac still sleeps (polls
then happen only on its dark wakes); it notes when the board stops
answering. Afterwards, compare
the last good poll with `pmset -g log` and the heap trend.

## 3. When it hangs again, before resetting (no code needed)
1. Wave at the ToF sensor: a green flash means the LED and ToF goroutines run.
2. Flip a wall switch: the light switching means the bus goroutine runs.
3. Hold A on + B off for 5 s: LED2 pulsing blue means the console goroutine
   runs (the gesture is polled in `runConsole`), and the console is on BLE.
4. `make nus`, then `sys`: the USB queue and heap numbers above.

USB-only (case 1) = steps 1-4 work and `usb` is full and losing bytes; the
fix then goes into a TinyGo patch under `patches/` (clear the CDC pump and
EasyDMA state on USB reset/resume). Whole firmware (case 2) = steps fail;
the soak log's heap trend says where to look next.

## Verification
- `make test` passes, including the new `outq` test.
- `make flash` (or `make ota`), then `sys` on USB and on BLE (`make nus`)
  prints sane values; `sys foo` prints the usage.
- The soak script logs a first poll, then one every 5 min.
- Commit the firmware change only when the user asks.
