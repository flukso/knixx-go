# Wall switches A and B on GPIO interrupts

> **Status: completed.** Implemented in commit `4307864` ("drive the
> switches from the wall toggles"), followed by `8a1e396` (2026-10-19).
> Recovered from the planning session's transcript: the session reused one
> plan file for all its plans, so only the last one was left on disk.

## Context

The thunder board connects two dual-throw momentary (centre-off) toggle
switches to the Feather. The contacts are debounced in hardware, active low,
and have no external pull-ups:

| contact | Feather pin | nRF pin |
|---|---|---|
| A on  | A0  | P0.04 |
| A off | A1  | P0.05 |
| B on  | D9  | P0.26 |
| B off | D10 | P0.27 |

None of these pins is used elsewhere. The firmware uses A3 (LEDs), P0.13–15
(ToF), QSPI and I2C.

Requirements:
- **Single press.** Each press does exactly what `switch <A|B> <on|off>` does
  on the console.
- **NUS on.** Holding A on and B off together for more than 5 s does `nus on`.
- **NUS off.** Holding A off and B on together for more than 5 s does `nus
  off`.
- **Hold side effect is accepted.** A hold still fires the two single presses
  when it starts.

## Design

This follows the existing ToF pattern in `src/nrf/tof.go`:
- The interrupt handler only increments an `atomic.Uint32`.
- A goroutine polls the counters and does the work in thread context.

That avoids channel operations and allocations inside an interrupt.

1. **Interrupts.** Each pin is set up with `PinConfig{Mode:
   machine.PinInputPullup}` and `SetInterrupt(machine.PinFalling, …)`. This
   takes 4 GPIOTE channels; with the ToF INT, 5 of the 8 are in use. A falling
   edge is a press. Debouncing in hardware means one edge per press.
2. **Single presses.** A `runSwitches` goroutine polls every 20 ms. For each
   contact whose counter changed, it runs the switch action once. Several
   edges within one poll period collapse into one action, which is fine
   because the action sets a state rather than toggling it.
3. **Holds.** These are detected from the pin levels in the same poll loop,
   not from interrupts. A pattern is active while both of its pins read low.
   It fires once after 5 s of continuous activity and re-arms only after the
   pattern is released. Edge counters ignore levels that are already low at
   boot, so a stuck contact causes no spurious press, though a stuck pair
   would count toward a hold.
4. **Same code path as the command.** Factor the body of the `switch` command
   in `src/cli/cli.go` into an exported `cli.Switch(dev, cmds, name string, on
   bool, w io.Writer)`. Both the command and the GPIO path call it, so name
   lookup, the enqueue and the `tx queue full` / `unknown switch` messages are
   identical. Messages go to `console`. A successful press prints nothing, as
   on the console.
5. **NUS stays on the console goroutine.** `nusOn` and `nusOff` change
   `active`, `editor` and `greeted`. These belong to the console goroutine,
   which can yield in the middle of a command's output.
   - The switch goroutine only posts a request in an `atomic.Int32` (none, on
     or off), last request wins.
   - `runConsole` picks it up next to `pollNUS()` (`src/nrf/console.go`).
   - To reuse the command exactly, the `on` and `off` branches of `nusCmd`
     (`src/nrf/nus.go`) move into `nusStart(w)` and `nusStop(w)`. Both the
     command and the request handler call them, keeping "already on", "not on"
     and the move messages.
   - The handler prints a short cause line first (`switches: A on + B off held
     5 s`) and then re-prints the prompt, like `pollNUS` does for its idle
     timeout.

### Files

- **`src/switches/switches.go` (new, host-testable, no build tag).** The pure
  logic:
  - a `Hold` detector with `Update(now time.Time, active bool) (fire bool)`;
  - a small edge-counter tracker, `Edges.Changed(n uint32) bool`.

  `src/switches/switches_test.go` covers:
  - fires at more than 5 s, but not at 4.9 s;
  - fires only once per hold;
  - re-arms after release;
  - a release before 5 s resets the timer;
  - counter wrap.
- **`src/nrf/switches.go` (new, `//go:build tinygo`).**
  - The pin table (pin, switch name, on/off).
  - The counters and `runSwitches(dev, cmds)`.
  - The NUS request atomic.
  - If `SetInterrupt` fails, it calls `fail` and the goroutine exits.
- **`src/nrf/main.go`.** Add `go runSwitches(dev, cmds)` next to `runToF`.
- **`src/nrf/nus.go`.**
  - Extract `nusStart(w)` and `nusStop(w)`.
  - Add `pollSwitchNUS()`, called from the console loop.
- **`src/nrf/console.go`.** Call the request handler in the `runConsole` loop.
- **`src/cli/cli.go`.** Add `Switch(...)`; the `switch` command calls it.
  Extend `cli_test.go` with a direct `cli.Switch` case: an unknown name, and a
  full queue.
- **`README.md`.** A short "Wall switches" paragraph: the pin map, single
  presses, and the two 5 s hold gestures for NUS.

## Verification

1. **Tests and build.** Run `make test` (the new `switches` tests and the
   `cli` tests), then `make nrf`. Check that flash and RAM grow only modestly
   over 312236 / 22132.
2. **Flash and watch the console.** Run `make flash` and use screen on
   `/dev/ttyACM0` with `sniff on`:
   - Each of the four contacts sends one group write: A on/off → 0/0/1, B
     on/off → 0/0/2.
   - The lights follow, the same as typing `switch A on` and the rest.
   - Rapid repeated presses never send a duplicate per press.
3. **NUS on by hold.** Hold A on + B off. The A on and B off telegrams appear
   at once; after 5 s the NUS message appears and the BLE LED shows. Keep
   holding: it doesn't fire again. Connect from a phone with nRF Connect to
   confirm.
4. **NUS off by hold.** Hold A off + B on for 5 s. The console returns to USB
   with the same messages as `nus off`. Repeat while connected over BLE.
5. **Short hold.** Release after about 3 s: nothing happens besides the two
   presses.
6. **Coexistence.** ToF motion and the sensor keep working (`tof`, `stat`),
   and the console doesn't stall while switches are pressed during console
   output.
7. **Commit** when you say so.
