# Configurable min/max detection distance for the ToF motion indicator

> **Status: completed.** Implemented in commit `15163e7` ("publish VL53L7CX
> motion as KNX occupancy") (2026-10-18). Recovered from the planning
> session's transcript: the session reused one plan file for all its plans,
> so only the last one was left on disk.

## Context

The motion indicator only looks for motion inside a distance window.
`MotionIndicatorInit`
in `src/internal/vl53l7cx/motion.go` hard-codes the ULD default of 400–1500
mm. The user wants
the window set in the `[occupancy]` section of `knixx.ini`, to tune what
counts as occupancy:
- excluding a doorway behind the area of interest;
- ignoring things close to the sensor;
- covering a larger room.

What the sensor supports, from
`vl53l7cx_motion_indicator_set_distance_motion()` in the ULD,
`src/vl53l7cx_plugin_motion_indicator.c`:
- **Limits:** min ≥ 400 mm, max ≤ 4000 mm, and max − min ≤ 1500 mm.
- **Two derived fields** of the 156-byte motion config:
  - `ref_bin_offset = (int32)((min/37.5348 − 4) × 2048.5)`
  - `feature_length = (uint8)(((max − min)/10 + 30.02784)/15.01392 + 0.5)`
- The result is written to `DCI_MOTION_DETECTOR_CFG`.
- **ULD bug:** C computes `max − min` as a promoted int, so it accepts max <
  min. The Go
  port will reject that, and will also require max > min.

**Decision (user):**
- Config checks each key only against 400..4000, so keys can be set in any
  order and
  `config dump` replays cleanly.
- When the pair is invalid (max ≤ min, or max − min > 1500), the ToF keeps the
  last valid
  window (400–1500 at boot) and `tof` shows a warning.
- The file loader also accepts any in-range pair, so a saved file never stops
  the device from
  booting.

## New keys

| key | unit | default | per-key range |
|---|---|---|---|
| `occupancy.min_distance` | mm | 400 | 400..4000 |
| `occupancy.max_distance` | mm | 1500 | 400..4000 |

`Format` and `default.ini` order: group, timeout, threshold, frequency,
integration,
min_distance, max_distance.

## Changes

1. **`src/internal/vl53l7cx/motion.go`**: keep the motion config in `Device`
   so that it can
   be rewritten.
   - Add to `Device` (in `vl53l7cx.go`) a small `motion struct{ resolution
     int;
     refBinOffset int32; featureLength uint8 }`. Don't hold the 156 bytes;
     `d.buf()` is
     clobbered by every later DCI access.
   - Factor the body of `MotionIndicatorInit` into `writeMotionConfig()`. It
     builds the
     156 bytes in `d.buf()[4:]` from those fields, exactly as today.
   - `MotionIndicatorInit(res)` sets refBinOffset 13633, featureLength 9 and
     the
     resolution, then calls `writeMotionConfig`. Its transactions stay
     byte-identical.
   - New exported constants `MinMotionDistance = 400`, `MaxMotionDistance =
     4000` and
     `MaxMotionSpan = 1500`.
   - New `func ValidMotionDistance(min, max uint16) bool`: true when 400 ≤
     min, max ≤ 4000,
     max > min and max − min ≤ 1500.
   - New `func (d *Device) SetMotionDistance(min, max uint16) error`:
     - returns `ErrInvalid` before any bus traffic when the pair is invalid,
       or when
       `MotionIndicatorInit` hasn't run;
     - computes both fields with `float32` arithmetic. Every step is wrapped
       in an explicit
       `float32(...)` conversion so Go can't fuse operations, which matches
       C's
       `float_t` results bit for bit;
     - then calls `writeMotionConfig`.

2. **`src/thunder/thunder.go`**
   - Add `MinDistance` and `MaxDistance uint16` to `Occupancy`.
   - Add the constants `DefaultMinDistance = 400`, `DefaultMaxDistance =
     1500`,
     `MinMotionDistance = 400`, `MaxMotionDistance = 4000` and `MaxMotionSpan
     = 1500`.
   - New method `(o Occupancy) DistanceValid() bool`, with the same rule as
     the driver.
     thunder must stay importable by host code without the driver, so this
     duplicates the
     check deliberately; a test keeps the two in step.

3. **`src/config/config.go`**, **`default.ini`** and **`config_test.go`**: the
   same pattern as
   frequency and integration.
   - Key constants, Parse defaults and the optional-key loop.
   - `Keys` and `field` cases with `ref{number, min: 400, max: 4000}`.
   - Format: the omission condition and two more lines.
   - Add `min_distance = 400` and `max_distance = 1500` to `default.ini`.

4. **`src/nrf/tof.go`**
   - `rangingSettings` gains `minDistance` and `maxDistance`; `settingsOf`
     copies them.
   - **Effective window:** `settingsOf` (or a helper `effectiveWindow`)
     substitutes the
     last applied valid window when `DistanceValid()` is false, and 400/1500
     at the first
     start. This way an invalid pair never reaches the driver, and `startToF`
     can't loop
     on ErrInvalid.
   - `startToF`: insert the step
     `tof.SetMotionDistance(s.minDistance, s.maxDistance)` right after
     `MotionIndicatorInit`.
   - `applyToF`: when the window changed, call `SetMotionDistance` between
     Stop and Start.
   - `tofCmd`:
     - print `motion window %d–%d mm`;
     - when the configured pair in `dev.Occupancy` is invalid, add
       `warning: min_distance/max_distance invalid (need min < max, span ≤
       1500 mm); keeping %d–%d mm`.
     - tofCmd needs access to `dev`; pass it in, as `runToF` already gets it.

## Tests

- **Golden test** (`src/internal/vl53l7cx/testdata/harness/harness.c`):
  - after `vl53l7cx_motion_indicator_init`, call
    `vl53l7cx_motion_indicator_set_distance_motion(&dev, &motion, 1000,
    2400)`;
  - regenerate `testdata/configure.golden` with the existing `go generate`;
  - `configure()` in `vl53l7cx_test.go` gains `d.SetMotionDistance(1000,
    2400)` at the same
    place.
  - This proves the float math and the rewritten config match C byte for byte.
- **Driver table test:**
  - the C formula outputs for (400,1500), (400,1900), (1000,2500) and
    (2500,4000) are
    13636/9, 13636/12, 46382/12 and 128246/12 respectively;
  - check them by building the config and inspecting `d.buf()`, or by an
    unexported
    `motionFields(min, max)` helper returning both values;
  - `TestInvalid` gains (399,1000), (1000,4001), (1000,2501) span, (1500,1500)
    and
    (1500,1000), plus `SetMotionDistance` before init. None may cause
    transactions.
- **Agreement test:** a test in vl53l7cx and one in thunder over a shared
  table of pairs
  keeps `DistanceValid` and `ValidMotionDistance` in step.
- **Config:** `TestOccupancy` covers:
  - defaults, round trip, and Set/Get of both keys;
  - range errors (399, 4001, non-numeric) through Set and Parse;
  - that the invalid pair 2500/1500 is accepted by Set, Format and Parse;
  - that an old section without the keys loads the defaults.
  `TestFormatMatchesDefault` keeps default.ini in step.

## Notes

- The window is not a hard cut-off on target distance: it sets the range of
  histogram bins
  the motion detector compares.
- At the default 400–1500 the formula gives ref_bin_offset 13636, against the
  init's
  hard-coded 13633. That's about 0.05 mm and harmless. Calling
  `SetMotionDistance`
  unconditionally at start keeps the code simple.
- Motion values depend on distance and reflectance (UM3038 §4.13), so moving
  the window
  farther out may need a lower `threshold`.

## Verification

1. `make test` passes, including the regenerated golden comparison.
2. `make nrf` builds.
3. On the board:
   - `tof` shows `motion window 400–1500 mm`.
   - `config set occupancy.max_distance 2500` sets a valid span of 1500, and
     `tof` shows the
     new window.
   - Motion at about 2 m now triggers; it didn't before.
   - `config set occupancy.min_distance 2600` makes min > max: `tof` warns and
     keeps
     400–2500.
   - `config set occupancy.max_distance 4000` makes the pair valid again, and
     the window
     becomes 2600–4000.
4. Settings survive a reboot.
