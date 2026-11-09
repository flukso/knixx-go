# Status LEDs: WS2812B pair on P0.28

> **Status: completed.** Implemented in commit `1ec8245` ("show init,
> occupancy and errors on the WS2812B status LEDs") (2026-10-18). Recovered
> from the planning session's transcript: the session reused one plan file
> for all its plans, so only the last one was left on disk.

## Context

Two WS2812B LEDs sit in a string on P0.28, which is Feather pin A3. They tell
the user what the device is doing. A single goroutine owns both LEDs, and the
only way to reach it is a Go channel of triggers.

Each trigger starts a *mode*. Triggers are queued and play one after another
in order, each mode to its timeout. A dedicated cancel trigger ends a mode
early. Senders never block: a trigger sent to a full channel is dropped.

LED2 stays off for now.

| Mode | LED1 behaviour (red) | Length | Ends early |
|---|---|---|---|
| Init | blinks at 5 Hz: 100 ms on, 100 ms off | 10 s | cancelled after the first ToF start |
| Occupancy | one 50 ms pulse, then 50 ms off | 100 ms | — |
| Error | blinks at 10 Hz: 50 ms on, 50 ms off, 5 flashes | 0.5 s | — |

Occupancy's 50 ms dark tail keeps back-to-back pulses separate. Init and Error
already end on an off phase.

## Existing pieces to reuse

- **WS2812 driver.** `tinygo.org/x/drivers/ws2812` v0.36.0 is already a module
  dependency.
  - `ws2812.NewWS2812(pin)` needs the pin configured as an output first.
  - `Device.WriteColors([]color.RGBA)` sends GRB.
  - At 64 MHz it disables interrupts per byte, about 10 µs at a time and about
    60 µs for both LEDs. That is harmless for the UART at 38400 baud and for
    USB.
- **Pin.** `machine.A3` is P0_28, defined in
  `tinygo/root-0.42.0/src/machine/board_feather-nrf52840.go`.
- **Error report sites.** These are the places that currently only print:
  - `src/nrf/sensor.go`: sht20, vcnl4040 and "tx queue full".
  - `src/nrf/tof.go`: vl53l7cx and "tx queue full".
  - `src/nrf/storage.go` (`loadConfig`) and `src/nrf/main.go`
    (`configureI2C`): errors in the boot log.
  - `src/bus/bus.go`:
    - "no switch";
    - `Pack`/`TxData`/`Write` errors;
    - negative or missing L_Data.con;
    - missing U_State or U_SystemStat reply.
- **Non-blocking sends.** The `select { case ch <- x: default: }` idiom
  already used for `cmds` in `sensor.go` and `tof.go`.

## Changes

### 1. New package `src/led`, host-testable

- **Types**
  - `type Mode uint8`, with the values `Init`, `Occupancy` and `Error`.
  - `type Trigger struct{ Mode Mode; Cancel bool }`.
  - The patterns live in a table indexed by Mode: half-period, on/off shape
    and length. The constants come from the mode table above.
- **`Notify(ch chan<- Trigger, t Trigger)`**
  - It sends with `select … default` and drops the trigger when the channel is
    full.
  - `const QueueLen = 8` sets the channel capacity.
- **`Player`** is the pure sequencer. It takes no time or hardware calls, so
  tests drive it with synthetic times.
  - **State:** the current mode and its start time, plus a fixed ring of
    pending modes, `[QueueLen]Mode`. A trigger that arrives with the ring full
    is dropped.
  - **`Apply(t Trigger, now time.Time)`**
    - A plain trigger is appended to the ring. If the player is idle, that
      mode starts at `now`.
    - A cancel ends the current mode if it matches, and the next pending mode
      starts at `now`. It also removes pending entries of that mode.
      Cancelling a mode that is neither playing nor pending does nothing.
  - **`Frame(now time.Time) (led1 color.RGBA, next time.Time, busy bool)`**
    - It first moves past any modes that have ended. The next one starts at
      the previous one's end time, so the sequence stays gapless even if the
      goroutine wakes late.
    - It returns LED1's colour and the time of the next on/off edge or mode
      end.
    - `busy` is false when nothing is playing or pending.
  - **Phase:** blinks are timed from the start of the mode.
  - **Colour:** `Red = color.RGBA{R: 0x20}`, a constant, because full red is
    glaring. LED2 is always black.
- **`Run(triggers <-chan Trigger, strip Strip)`**
  - `Strip` is `interface{ WriteColors([]color.RGBA) error }`, which
    `ws2812.Device` satisfies.
  - At start it writes both LEDs black, because the LEDs keep their last state
    across an MCU reset.
  - The loop uses one reused `time.Timer` and a `select` on the trigger
    channel and the timer:
    - It drains the trigger channel into the Player. Cancels therefore act at
      once, even while another mode plays.
    - It rewrites the strip only when LED1's colour changes.
    - It re-arms the timer for `next`.
  - When nothing is busy, it stops the timer and blocks on the channel only.
    That means no idle wake-ups.

### 2. `src/bus/bus.go`

- **Hook:** add `var OnError func()`, nil by default, so `bus` does not depend
  on `led`.
- **`errorf(format, args...)`:** a small helper that prints to `Out` and calls
  the hook. It replaces the error `Fprintln(Out, …)` lines.
- **What doesn't count as an error:** the informational `dump` and sniff
  output.

### 3. `src/nrf`

- **New `led.go`**
  - Defines `ledPin = machine.A3 // P0.28: WS2812B data, LED1 then LED2` and
    `var leds chan led.Trigger`.
  - `startLEDs()`:
    - configures the pin as an output and drives it low;
    - creates the channel with capacity `led.QueueLen`;
    - starts `go led.Run(leds, ws2812.NewWS2812(ledPin))`.
  - `fail(format string, args ...any)` prints to `console` and sends
    `led.Notify(leds, Trigger{Mode: led.Error})`.
- **`main.go`**
  - Call `startLEDs()` first, then `led.Notify(leds, led.Trigger{Mode:
    led.Init})`.
  - Set `bus.OnError` to send Error.
  - When `loadConfig` or `configureI2C` reports an error, also send Error.
    Those functions write to the boot `log` buffer, so pass a flag back or use
    a small error-counting writer. I'll pick whichever reads cleaner.
- **`tof.go`**
  - **Occupancy:** `watchToF` sends one Occupancy trigger each time it sees
    new pulses. The 50 ms poll merges pulses that fall within one poll.
  - **Init end:** `runToF` sends `Trigger{Mode: led.Init, Cancel: true}` once,
    after the first `startToF` attempt, whether it succeeds or fails. On
    failure, `fail(…)` then queues Error, which plays right after the cancel.
  - **Error reporting:** the error prints become `fail(…)`.
- **`sensor.go`:** the error prints become `fail(…)`.

### 4. Tests: `src/led/led_test.go`

- **Edges:** each mode's on/off edges and its length. Occupancy is 50 ms on
  and ends at 100 ms; Error gives 5 flashes in 500 ms; Init gives 5 Hz for 10
  s.
- **Ordering:** sequential order, for example Error then two Occupancy pulses,
  back to back with no gap.
- **Cancel:**
  - stops the current Init and starts the next mode;
  - removes pending entries of that mode;
  - does nothing for an unknown mode.
- **Drops:** the ring drops triggers when full, and `Notify` doesn't block on
  a full channel.
- **LED2:** always black.
- **`Run`:** with a fake strip and short real timings, it writes only on
  changes and ends with both LEDs black.
- **`bus_test.go`:** `OnError` fires on a missing L_Data.con.

### 5. `README.md`

- A status-LED section with the mode table and the queue semantics.
- A hardware table row: WS2812B ×2 on A3/P0.28.
- An `led` entry in the code layout table.
- A note in the flow diagram.
- A caveat: WS2812Bs draw roughly 0.5–1 mA each even when dark.

## Verification

1. `make test` passes. `make nrf` builds, and the flash and RAM change stays
   modest.
2. On the board:
   - **Reset:** LED1 blinks red at 5 Hz until the ToF is ranging, about 2–3 s,
     then goes dark.
   - **Motion:** waving a hand gives short 50 ms flashes.
   - **Error:** unplugging the KNX bus and running `on`, or a similar send,
     gives the 0.5 s fast flash after the L_Data.con timeout.
   - **LED2:** stays dark throughout.
   - **ToF failure:** holding the ToF in reset or with no sensor, init stops
     and the error flash follows.
