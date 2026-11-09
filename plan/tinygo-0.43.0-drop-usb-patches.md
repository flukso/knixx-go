# TinyGo 0.43.0: drop our nRF52840 USB patches

> **Status: completed.** Implemented in commit `f6f36ca` ("build with TinyGo
> 0.43.0 and drop the USB patches") (2026-10-19). Recovered from the
> planning session's transcript: the session reused one plan file for all
> its plans, so only the last one was left on disk.

## Context

We build against TinyGo 0.42.0, with two local patches to
`src/machine/machine_nrf52840_usb.go` (`patches/`, applied by the Makefile to
a copy in `tinygo/root-0.42.0`):

1. **`tinygo-0.42.0-nrf52840-usbd-epdata.patch`** (ba65dae). The EPDATA loop
   used `if inDataDone {...} else if outDataDone {...}`. CDC uses one endpoint
   number for both directions, so an OUT that completed in the same interrupt
   as an IN was never fetched. The console stopped responding while browsing
   the history.
2. **`tinygo-0.42.0-nrf52840-usbd-single-dma.patch`** (968317a). Two bugs:
   - The ISR waited for the EasyDMA busy flag with `wfi`, but only a later
     part of the same ISR cleared it, so a paste hung the firmware.
   - IN transfers started from thread mode ignored the flag and could overlap
     an OUT transfer.

   The patch serialises all USBD DMA through `startDMA`/`waitDMA`, hands out
   finished OUT data before fetching the next packet, protects
   `ReceiveUSBControlPacket`, and applies the anomaly 199 workaround.

TinyGo 0.43.0 (PR #5794: "fix USB endpoint 3 and EasyDMA ownership, and clear
EPDATASTATUS before the EPDATA handlers run") rewrote this part of the driver.
I compared v0.42.0 with v0.43.0 of `machine_nrf52840_usb.go`.

## Findings: both patches are obsolete in 0.43.0

| our fix | 0.43.0 |
|---|---|
| IN and OUT handled independently (epdata) | Fixed. All IN bits are handled first, then every OUT bit goes into `epOutWaiting` and is started by `startWaitingOut()`; no `else if`. |
| No `wfi` wait inside the ISR (paste hang) | Fixed. `claimEasyDMA` polls `checkCompletions()` in interrupt context, releases ended transfers itself with interrupts disabled, and `gosched()`s in thread mode. The EPDATA OUT path never blocks: `tryClaimEasyDMA` or defer to `epOutWaiting`. |
| IN from thread mode serialised with OUT | Fixed. `sendViaEPIn` and `SendZlp` claim EasyDMA, which is tracked per owner (`easyDMAOwner`) and released only by that transfer's END event. |
| Finished OUT handed out before the next fetch | Fixed. `checkCompletions()` runs first in every ISR, and the ISR returns early while EasyDMA is busy. |
| Clear all EPDATASTATUS bits up front | New in 0.43.0. This fixes a lost-completion race that we didn't patch, made likely by SoftDevice radio events, which matters for our BLE console. |

**Not carried over** (small residual risk, no symptom seen):
- **No anomaly 199 workaround.** 0.43.0 no longer triggers DMA tasks while a
  transfer runs, with one exception: `ReceiveUSBControlPacket` (EP0 OUT data
  stage, e.g. CDC SET_LINE_CODING) and `TASKS_EP0STALL` don't claim EasyDMA.
  An OUT DMA started by `startWaitingOut()` earlier in the same ISR could
  still be running. That needs a line-coding request and bulk OUT data in the
  same interrupt, for example opening the port while text is still arriving.
- **No timeout on the DMA wait.** EasyDMA ends independently of the bus, so
  this is defensive only.

If hardware testing shows a problem in that area, a small 0.43.0 patch (claim
EasyDMA around `ReceiveUSBControlPacket`) would replace both current ones.

Other 0.43.0 changes relevant to us:
- **#5816:** an 8 KB default stack on nRF52840. Our `STACK_SIZE := 8KB` stays,
  as an explicit setting.
- **#5832:** a missed RTC wakeup fix.
- **#5851:** stopping the RNG after each read.
- **Precise GC layouts:** a heap behaviour change, worth watching.

MSC (#5794) is an opt-in package, so it doesn't affect us.

## Upgrade steps

1. **You:** install TinyGo 0.43.0 to `/usr/local/lib/tinygo` (needs root).
2. **Makefile:** `TINYGO_VERSION := 0.43.0`.
   - Keep the patched-root mechanism. With no `patches/tinygo-0.43.0-*.patch`
     it copies the root unpatched, which keeps the version check and the slot
     for future patches.
   - Update the `STACK_SIZE` comment: 8 KB is now the nRF52840 default, and we
     keep it explicit.
3. **`git rm` the two 0.42.0 patches.**
4. **README "Patched TinyGo":** state 0.43.0 and that no patches are needed
   now, because the USB fixes are upstream since 0.43.0 (#5794).
5. **Dependencies:** check `tinygo.org/x/bluetooth` v0.16.0 and drivers
   v0.36.0 build with 0.43.0. Bump them only if the build fails.
6. **Cleanup:** remove the old `tinygo/root-0.42.0` copy. It's untracked; ask
   before deleting.

## Verification

1. `make test`, then `make nrf`. Compare flash and RAM with the 0.42.0 build:
   308460 bytes of flash.
2. `make flash`, then use screen on `/dev/ttyACM0` and repeat the scenarios
   behind the old patches:
   - **History:** browse the history quickly with up and down while output
     runs (the epdata case).
   - **Paste:** paste a long block of text several times (the single-DMA
     case).
   - **Line coding:** reopen screen repeatedly while typing (the residual
     SET_LINE_CODING race).
3. **BLE together with USB:** `nus on`, keep the USB port open, run console
   traffic over NUS while pasting on USB, and run `nus off`. This covers the
   EPDATASTATUS race with SoftDevice events.
4. **OTA and version:** `make ota` and the `version` command still work, which
   also confirms the `-ldflags -X` embedding under 0.43.0.
5. **Bus and sensors:** a soak of a few hours with sniff, occupancy and sensor
   publishing, watching for LED errors or console stalls.
6. **Commit** when you say so: the Makefile, README and patch removal.
