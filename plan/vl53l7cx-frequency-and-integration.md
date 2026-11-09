# Configurable VL53L7CX ranging frequency and integration time

> **Status: completed.** Implemented in commit `15163e7` ("publish VL53L7CX
> motion as KNX occupancy") (2026-10-18). Committed together with the
> min/max detection distance. Recovered from the planning session's
> transcript: the session reused one plan file for all its plans, so only
> the last one was left on disk.

## Context

The ToF code in `src/nrf/tof.go` (`startToF`) hard-codes 1 Hz and 5 ms. The
user wants both
settings in the `[occupancy]` section of `knixx.ini`, so they can be tuned to
trade power
against responsiveness and sensitivity.

They behave like `threshold` does today:
- each value is range-checked by `config set` and by the file loader;
- a missing key falls back to its default;
- a change is applied live, without a reboot.

**Decision (user):** each key is checked only against its own range. Any
combination loads,
whatever order the keys are set in. When integration time + 1 ms is not
shorter than the
ranging period (1000/frequency ms), the sensor stretches the period itself
(UM3038 §4.6), and
the `tof` command reports this.

## New keys

| key | unit | default | range | source of the range |
|---|---|---|---|---|
| `occupancy.frequency` | Hz | 1 | 1..60 | UM3038 §4.4 (4x4: 1–60 Hz) |
| `occupancy.integration` | ms | 5 | 2..1000 | UM3038 §4.6, the ULD |

`default.ini` and `Format` order: group, timeout, threshold, frequency,
integration.

## Changes

1. **`src/thunder/thunder.go`**
   - Add `Frequency` and `Integration uint16` to `Occupancy`, with doc
     comments.
   - Add the constants `DefaultRangingFrequency = 1`, `MinRangingFrequency =
     1`,
     `MaxRangingFrequency = 60`, `DefaultIntegrationTime = 5`,
     `MinIntegrationTime = 2` and
     `MaxIntegrationTime = 1000`.
   - Add a helper `func (o Occupancy) PeriodStretched() bool`, true when
     `uint32(o.Integration)+1 >= 1000/uint32(o.Frequency)`. It is
     host-testable and used by
     `tof`.

2. **`src/config/config.go`**: reuse the existing `ref{number, min, max}`
   mechanism.
   - Add the constants `frequencyKey = "frequency"` and `integrationKey =
     "integration"`.
   - `Parse`: seed the two defaults next to Timeout and Threshold, and add
     both keys to the
     loop over optional occupancy keys.
   - `Keys`: append `"occupancy.frequency", "occupancy.integration"`. `config
     dump` and
     `list` pick them up from there.
   - `field`: two new cases, with their min and max.
   - `Format`:
     - the section-omission condition also compares the two new values against
       their
       defaults;
     - print both lines after threshold.
   - Update the `Set` doc comment.

3. **`src/config/default.ini`**: add `frequency = 1` and `integration = 5`
   after
   `threshold = 44`.

4. **`src/internal/vl53l7cx/vl53l7cx.go`**: `SetRangingFrequency` gets a range
   check.
   - Return `ErrInvalid` for hz 0 or hz > 60 before any bus transaction, and
     update the doc
     comment.
   - The check doesn't depend on resolution, since the driver doesn't track
     it; 8x8 limits
     stay the caller's job, as documented.
   - `SetIntegrationTime` already checks 2..1000.

5. **`src/nrf/tof.go`**
   - Replace `tofStatus.threshold` with a `settings thunder.Occupancy`
     snapshot, or add the
     two fields next to it.
   - `startToF(o thunder.Occupancy)` passes `o.Frequency` and `o.Integration`
     to the existing
     `SetRangingFrequency` and `SetIntegrationTime` steps.
   - `watchToF`:
     - Compare threshold, frequency and integration with the applied settings.
     - On any change, call `StopRanging`, then only the setters whose value
       changed, then
       `StartRanging`, and record the new settings.
     - The ULD accepts these setters while stopped; the settings take effect
       at start.
   - `runToF`: snapshot all three from `dev.Occupancy` before `startToF`.
   - `tofCmd`:
     - print `ranging 4x4 at %d Hz, integration %d ms, threshold %d, …`;
     - when `PeriodStretched()` is true, add the line
       `warning: integration time + 1 ms exceeds the ranging period; the
       sensor lowers the frame rate`.
   - Update the `runToF` doc comment (no longer fixed at 1 Hz / 5 ms).

## Tests

- **`src/config/config_test.go`**
  - `TestOccupancy` covers the defaults for the new fields, round trip through
    Format/Parse, and Set/Get of both keys.
  - Out-of-range errors: frequency 0 and 61, integration 1 and 1001, and
    non-numeric values,
    both through `Set` and in an ini file.
  - A file with an occupancy section that lacks the new keys loads the
    defaults.
  - Format leaves out the section when all values are defaults and the group
    is unbound.
- **`TestFormatMatchesDefault`** keeps `default.ini` and `Format` in step
  automatically.
- **`src/thunder`**: a small table test for `PeriodStretched`:
  - 1 Hz / 5 ms → false
  - 60 Hz / 15 ms → false (15+1 < 16)
  - 60 Hz / 16 ms → true
  - 10 Hz / 99 ms → true
  - 2 Hz / 400 ms → false
- **`src/internal/vl53l7cx/vl53l7cx_test.go`**: `TestInvalid` gains
  `SetRangingFrequency(0)`
  and `(61)`, with no transactions. `TestMatchesULD` is unchanged; it still
  configures with
  1 Hz / 5 ms.

## Verification

1. `make test` (host `go test ./...`) passes.
2. `make nrf` builds.
3. On the board:
   - `config get occupancy.frequency` returns `1`.
   - `config set occupancy.frequency 61` is rejected.
   - `config set occupancy.frequency 10` followed by `tof` shows 10 Hz; motion
     frames come
     faster while moving.
   - `config set occupancy.integration 200` makes `tof` show the
     stretched-period warning.
   - `config dump` lists both keys.
4. After a reboot, the settings persist from littlefs.

Note for the user: motion values scale with integration time (UM3038 §4.13),
so changing
`integration` usually needs a re-tuned `threshold`.
