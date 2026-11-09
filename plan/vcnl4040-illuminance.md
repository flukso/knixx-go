# VCNL4040 ambient light → KNX DPT 9.004 illuminance

> **Status: completed.** Implemented in commit `9412700` ("publish VCNL4040
> illuminance on KNX") (2026-10-18). Recovered from the planning session's
> transcript: the session reused one plan file for all its plans, so only
> the last one was left on disk.

## Context

The Thunder Sensor board carries a Vishay VCNL4040 (U1), wired as follows:
- on I2C1 at 7-bit address 0x60, shared with the SHT20 (0x40) and the VL53L7CX
  (0x29);
- its INT pin goes to PROX.INT, on Feather pin A2.

We want its ambient light sensor (ALS) published on KNX as DPT 9.004 (lux) to
a group
address configured in `knixx.ini`. The proximity sensor stays off, and the
part must stay
low-power.

**Decisions (user):**
- **Interval:** measure every 60 s on the SHT20's rhythm, with independent
  error handling.
- **Integration time:** fixed at 80 ms, giving 0–6553.5 lx at 0.1 lx/step.

## Low-power approach (datasheet facts)

- **Current draw:** with the ALS running and PS off, the part draws about 260
  µA
  (I_PSSD); fully shut down it draws 0.2 µA. The ALS has no one-shot or
  "active force"
  mode; only PS has one. ALS interrupts would need the ALS running
  continuously.
- **One-shot by toggling ALS_SD**, once per minute:
  1. Write ALS_CONF = IT 80 ms, ALS_SD = 0.
  2. Wait 2 × IT (160 ms). The datasheet gives no settle time, so this is a
     conservative
     margin to check on hardware.
  3. Read ALS_Data.
  4. Write ALS_SD = 1.

  The part is on for about 0.17 s/min, about 0.7 µA on average, plus 0.2 µA in
  shutdown.
- **On a failed read**, still attempt the shutdown write, best-effort, so an
  error never
  leaves the ALS running.
- **PS stays off:** PS_SD = 1 is written explicitly in Configure, because an
  MCU reset
  doesn't power-cycle the sensor.
- **INT stays unused:** the ALS interrupt stays disabled (ALS_INT_EN = 0), so
  INT stays
  high. A2 is left unconfigured.

## Changes

1. **New driver `src/internal/vcnl4040/vcnl4040.go`**, in the style of
   `internal/sht2x/sht2x.go`. It takes a `drivers.I2C`, and `Sleep` can be
   injected for tests.
   - **Registers** (16-bit, accessed with SMBus read and write word, LSB
     first):
     - `ALS_CONF` 0x00 (low byte): IT bits 7:6, PERS bits 3:2, INT_EN bit 1,
       SD bit 0.
     - `PS_CONF1_2` 0x03; `PS_CONF3_MS` 0x04; `ALS_DATA` 0x09; `ID` 0x0C,
       expected 0x0186.
   - **`Address = 0x60`**, **`MaxLux10 = 65535`**, and
     **`Device{bus, Address, Sleep}`** with **`New(bus)`**.
   - **`Configure()`:**
     - read ID; on a mismatch, return `ErrID`;
     - write `PS_CONF1_2 = 0x0001`, which sets PS_SD with PS_CONF2 at its
       default;
     - write `PS_CONF3_MS = 0x0000`;
     - write `ALS_CONF = 0x0001` (IT 80 ms, interrupt off, shut down).
   - **`ReadIlluminance() (lux10 uint32, err error)`:** the one-shot above. It
     returns the
     raw count, which at 80 ms is exactly tenths of a lux (0.1 lx/step).
     Shutdown runs
     whether or not the read fails, and the read error takes precedence.
   - **`internal/vcnl4040/vcnl4040_test.go`:** a fakeBus that logs
     transactions, as in the
     sht2x tests. Tests:
     - the Configure sequence, and the ID mismatch;
     - the read sequence: enable write, sleep 160 ms, read 0x09, shutdown
       write;
     - byte order;
     - a read error still produces the shutdown write;
     - a bus error.

2. **`src/thunder/thunder.go`:** `Sensor` gains `Illuminance uint16`, a group
   address with 0
   meaning unbound.

3. **`src/config/config.go`**, **`default.ini`** and **`config_test.go`**: the
   same pattern as
   `[temperature]` and `[humidity]`.
   - `illuminanceSection = "illuminance"`, added to the optional-group loop in
     Parse.
   - `Keys` gets `"illuminance.group"` after `humidity.group`; `field` gets a
     matching case.
   - Format writes `[illuminance]` only when the group is bound, after
     `[humidity]`.
   - `default.ini` adds `[illuminance]` with `group = 0/1/3`; a missing
     section means
     unbound.
   - `TestSensor` covers Get, Set, unbind, round trip and bad values for the
     new key.
     `TestFormatMatchesDefault` keeps `default.ini` in step.

4. **`src/bus/bus.go`** and **`bus_test.go`**
   - New `CmdLight` kind, with a `Command.Illuminance int32` field in 0.1 lx.
   - Run: when `cur.Sensor.Illuminance` is bound, queue
     `dpt.DPT_9004(float32(cmd.Illuminance)/10).Pack()`.
   - A separate kind from `CmdSensor` keeps the SHT20 and the VCNL4040
     independent.
   - Tests mirror the existing sensor test: the DPT 9.004 payload goes to the
     group, and
     nothing is sent while unbound.

5. **`src/nrf/sensor.go`**
   - **Driver:** a global `light vcnl4040.Device` and `lightConfigured bool`,
     with the
     driver created in `runSensor`.
   - **`measure()`:** the existing SHT20 block becomes `measureClimate`,
     unchanged, and
     `measureLight` is added. Both run on each tick, independently:
     - errors print `vcnl4040: …`;
     - a failure resets `lightConfigured`, so Configure runs again next tick;
     - success queues `CmdLight` without blocking ("tx queue full" otherwise).
   - **`reading`:** gains `lightValid`, `lightAt` and `lux10`.
   - **`sensor` command:** prints a second line,
     `illuminance %.1f lx at … (… s ago)`, with `(saturated)` when `lux10 ==
     65535`. The
     help text becomes "show the latest temperature, humidity and illuminance
     reading".

6. **`README.md`**
   - Functionality: an illuminance bullet.
   - Configuration: `illuminance.group` in the default block and in the table.
   - Caveats: the 6553 lx clip and the ±10% tolerance; no correction for a
     cover lens.
   - Packages table: `internal/vcnl4040`.
   - Hardware table: VCNL4040 on I2C1 at 0x60, with PROX.INT on A2 unused.
   - Flow: runSensor now also reads the VCNL4040.

## Notes

- No calibration factor for a window or lens over the sensor. It could later
  become an
  `illuminance.factor` key if readings turn out low.
- The 160 ms wait blocks only the sensor goroutine, which sleeps and yields.

## Verification

1. `make test` passes, including the new driver, config and bus tests.
2. `make nrf` builds.
3. On the board, after `config init`, or `config set illuminance.group 0/1/3`
   on an existing
   file:
   - Within about 5 s of boot, `sensor` shows an illuminance line.
   - It changes when the sensor is covered or lit with a phone torch. With a
     torch close by
     it may show `(saturated)`.
   - `sniff on` shows a DPT 9.004 write to 0/1/3 every minute, with a positive
     L_Data.con.
   - Optional: measure the board current to confirm there is no rise of about
     260 µA
     between readings, which would mean the ALS is left on.
