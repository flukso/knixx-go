# LED2: blue BLE pulse while NUS is active

> **Status: completed.** Implemented in commit `69a61a6` ("add a BLE console
> on the Nordic UART Service") (2026-10-18). Committed together with the BLE
> console. Recovered from the planning session's transcript: the session
> reused one plan file for all its plans, so only the last one was left on
> disk.

## Context

LED2 has been kept off until now. It should show that the BLE console (NUS) is
active, from `nus on` until `nus off` or the idle timeout:
- **Pattern:** a blue pulse at 1 Hz, 50 ms on and 950 ms off.
- **Colour:** `color.RGBA{B: 0x20}`, the same intensity as `Red` and `Green`
  on LED1.

LED1 keeps its sequential queue of init, occupancy and error. LED2 runs
independently of it, so a burst of LED1 modes never delays or hides the BLE
pulse.

## Design: `src/led/led.go`

- **New mode `BLE`.** `Notify(leds, Trigger{Mode: BLE})` starts the LED2
  pulse, and `Trigger{Mode: BLE, Cancel: true}` stops it. The trigger channel
  remains the only way in.
- **Colour:** add `Blue = color.RGBA{B: 0x20}` next to Yellow, Green and Red.
- **Pattern:** add a `BLE` entry `{colour: Blue, on: 50 ms, period: 1 s}`. A
  zero `length` means it runs until cancelled. `Apply` and `Frame` treat `BLE`
  as an LED2 mode, so it never enters the LED1 ring.
- **`Player` state:** a separate LED2 state, `led2On bool` and `led2Start
  time.Time`.
  - **`Apply`:**
    - `BLE` starts LED2 at `now`. A repeat while it is already running is
      ignored, so the phase stays steady.
    - `BLE` with Cancel stops LED2.
    - The LED1 queue and cancel logic is unchanged.
- **`Frame(now) (leds [2]color.RGBA, next time.Time, busy bool)`:**
  - `leds[0]` is LED1, as now.
  - `leds[1]` is the blue phase while LED2 is on, otherwise off.
  - `next` is the earlier of LED1's and LED2's next edge.
  - `busy` is true while either LED has something to play.
  - The shared phase logic, on for `on` at the start of each `period`, becomes
    a small helper used for both LEDs.
- **`Run`:** writes the strip whenever either LED changes, instead of LED1
  only. Everything else is unchanged; the timer and the idle wait on the
  channel stay as they are.
- **Package comment:** LED2 now shows the BLE console.

## Wiring: `src/nrf/nus.go`

- `nusOn()`: after advertising starts and the console is attached, send
  `led.Notify(leds, led.Trigger{Mode: led.BLE})`.
- `nusOff()`: send `led.Notify(leds, led.Trigger{Mode: led.BLE, Cancel:
  true})`. This covers both `nus off` and the 300 s idle timeout, since that
  calls `nusOff`.
- Both run on the console goroutine, and `Notify` never blocks.

## Tests: `src/led/led_test.go`

- **Update the existing tests** to the `[2]color.RGBA` return of `Frame`.
  - The `play` helper follows LED1 as before and asserts that LED2 stays off
    when `BLE` was not triggered.
  - `TestRun` expects LED2 off in every write.
- **`TestBLE`:**
  - After `BLE` at t0, LED2 is blue for [0, 50 ms), off until 1 s, and blue
    again at 1 s; `next` points at those edges.
  - `busy` stays true indefinitely.
  - Cancel turns LED2 off, and `busy` goes false when LED1 is idle.
  - A repeated `BLE` doesn't restart the phase.
- **`TestBLEIndependent`:** an LED1 Error sequence during the BLE pulse plays
  unchanged on LED1. LED2 keeps its 1 Hz phase, and `next` is the earliest
  edge of the two.
- **`TestRun`:** add a variant with a `BLE` trigger. Expect a write with LED2
  blue, then LED2 off 50 ms later, while LED1 stays off.

## README.md

- **Status LEDs:**
  - LED2 now shows a blue 50 ms pulse each second while the BLE console is on.
  - Replace "LED2 stays off".
  - Add the row `| ble | LED2: 50 ms blue pulse every 1 s | until nus off or
    the idle timeout | nus on |` to the table, and note that LED2 runs
    independently of the LED1 queue.
- **Flow item for `led.Run`:** mention LED2.
- **BLE console bullet:** mention the blue pulse.

## Verification

1. `make test` passes, with the new and updated `led` tests. `make nrf`
   builds.
2. On the board:
   1. `nus on`: LED2 pulses blue once a second, both while advertising and
      while connected.
   2. Motion and error flashes on LED1 don't disturb the LED2 rhythm.
   3. `nus off` over BLE: LED2 goes dark.
   4. `nus on` with no connection: LED2 goes dark when the 300 s timeout
      returns the console to USB.
   5. The blue pulse looks about as bright as the red and green on LED1.
