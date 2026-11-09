# Console lockup when pasting a `config dump` line

> **Status: completed.** Implemented in commit `968317a` ("fix the console
> hanging when pasting into it") (2026-10-17). Recovered from the planning
> session's transcript: the session reused one plan file for all its plans,
> so only the last one was left on disk.

## Context

Typing commands and browsing history work since the EPDATA patch (`ba65dae`).
Pasting a
line copied from `config dump` locks the console again. The difference is
timing:
- a typed key reaches the board as its own USB OUT packet, milliseconds after
  the last;
- a paste arrives as back-to-back OUT packets, microseconds apart. Linux
  cdc-acm splits a
  write into 64-byte packets, and many terminals also send a paste in several
  small
  writes.

The `config dump` lines can now be up to 97 bytes (MaxLine 128), so a pasted
line always
spans two packets.

## Root cause: a `wfi` spin inside the USBD interrupt handler

This is the second issue I flagged during the history investigation. It's in
`tinygo/root-0.42.0/src/machine/machine_nrf52840_usb.go`.

The driver serialises the USBD EasyDMA with a flag, `easyDMABusy`:
- `enterCriticalSection()` (l.34) waits with `for easyDMABusy { wfi }` and
  then sets the
  flag.
- It is called in `handleUSBIRQ` when an **EPDATA OUT** event starts the DMA
  from the
  endpoint buffer (`TASKS_STARTEPOUT`, l.212).
- The flag is cleared only by `exitCriticalSection()` in the **ENDEPOUT**
  loop, which runs
  *later in the same handler* (l.222-231).

Here is the sequence for two back-to-back OUT packets:

1. Packet 1 arrives. The IRQ sees EPDATA, sets busy and starts the DMA. The
   DMA hasn't
   finished when the handler reaches the ENDEPOUT loop, so it returns with
   busy still set.
   This is normal; the ENDEPOUT interrupt would clear it next time.
2. The DMA finishes and the OUT buffer is free. The host's packet 2 is
   accepted at once,
   so **ENDEPOUT and EPDATA are now both pending** when the next IRQ runs.
3. That IRQ handles EPDATA first. `enterCriticalSection` finds busy set and
   spins on `wfi`.
   The only code that would clear it is the ENDEPOUT loop further down the
   same handler.
   A pending USBD interrupt can't preempt itself, so `wfi` just returns into
   the loop
   forever.

The core is stuck inside the ISR at priority 2. Thread mode never runs again,
so
everything stops, not just the console:
- the console goroutine;
- the bus `rxPoll`;
- the sensor ticker.

Interrupts with a higher priority (lower number) still run, but no goroutine
does.

The window is a few microseconds per packet, so slow typing never hits it and
a paste
almost always does.

A related flaw: IN transfers (`sendViaEPIn`, l.333, called from thread mode
via
`USBCDC.Write` → `sendFromRing`) never take `easyDMABusy`. An echo's
`STARTEPIN` can
therefore start while an OUT DMA is still running. The nRF52840's USBD allows
only one
EasyDMA transfer at a time, so this can corrupt or lose a packet, though it
doesn't hang
by itself. The same tasks-during-DMA problem is the subject of nRF52840
anomaly 199
("USBD cannot receive tasks during DMA"); I'll check the anomaly text before
relying on
it.

## Confirming it (before changing anything)

1. **Is the whole MCU frozen?**
   - With a bus monitor, check whether the sensor telegrams on 0/1/1 and 0/1/2
     stop after
     the lockup.
   - Or check whether `switch` presses from another device still get an
     L_Data.con.
   - If the minute telegrams stop, the CPU is stuck in the ISR, which confirms
     this cause
     over a console-only problem.
2. **Is it length or timing?**
   - `printf 'config get device.individual\r' > /dev/ttyACM0` sends one short
     packet and
     should survive.
   - `printf '%s\r' "config set A.group 0/0/1, 0/0/2, 0/0/3, 0/0/4, 0/0/5,
     0/0/6, 0/0/7" > /dev/ttyACM0`
     is more than 64 bytes, so it goes as two back-to-back packets and should
     hang.
3. **Optional:** attach the debugger (SWD) and halt. The PC should be in
   `waitForEasyDMA`
   with IPSR = USBD.

## Fix: a second TinyGo patch, `patches/tinygo-0.42.0-nrf52840-usbd-dma.patch`

This uses the same mechanism as the first patch: the Makefile stamp depends on
`$(PATCHES)`, so `make nrf` rebuilds the patched root automatically.

Changes to `handleUSBIRQ` and its helpers:

1. **Finish a pending OUT DMA before starting another.**
   - Move the ENDEPOUT loop *above* the EPDATA block, so a completed DMA is
     consumed
     (handed to `usbRxHandler`, acked, busy cleared) before new data is
     fetched.
   - In the EPDATA path, a DMA can still be in flight, started at the end of
     the previous
     IRQ and not yet done. Replace the `wfi` spin with a short bounded poll on
     `EVENTS_ENDEPOUT[ep]`, then process that completion inline. A 64-byte DMA
     takes
     about a microsecond, and the event *will* come, because the hardware sets
     it without
     the CPU. `wfi` waits for an interrupt that can't be taken there.
   - Factor the ENDEPOUT body into `finishEPOut(ep)`, so the loop and the
     inline wait share
     it.
2. **Serialise IN DMA with OUT DMA.** `sendViaEPIn` is reached from thread
   mode via
   `USBCDC.Write`. Wrap the start in `interrupt.Disable()`/`Restore`, wait for
   a running
   OUT DMA to finish with the same poll, and only then set
   `PTR`/`MAXCNT`/`STARTEPIN`.
   - The IRQ path that starts an OUT DMA likewise waits for an in-flight IN
     DMA, using
     `EVENTS_ENDEPIN[ep]`, tracked with a second busy bit.
   - Then only one EasyDMA transfer runs at a time, as the hardware requires.
3. Never `wfi` in interrupt context; `waitForEasyDMA` becomes a bounded poll.

Testing:
- Host tests can't cover this, because it is hardware-only TinyGo code.
- Run `make nrf` to build. The build applies both patches; `patch --forward`
  fails loudly
  if they conflict.

## Verification (hardware)

1. Paste each `config dump` line, then all five at once. The console stays
   responsive,
   and every line answers `(saved)`, or `tx queue full` for the fast
   multi-line paste,
   which is expected.
2. Paste a 120-character line repeatedly. It must be echoed intact; check that
   no bytes
   are dropped or garbled, since that would mean the IN/OUT DMA overlap is
   still there.
3. Browse history while the sensor publishes, to check for regressions.
4. On the bus monitor, the minute sensor telegrams keep coming throughout.
