# VL53L7CX motion indicator → KNX occupancy (DPT 1.018)

> **Status: completed.** Implemented in commit `15163e7` ("publish VL53L7CX
> motion as KNX occupancy") (2026-10-18). The plan was given as a chat
> answer, not in plan mode, so it is taken from the conversation; the user's
> replies follow it. The ranging frequency, integration time and min/max
> distance became configurable in the same commit, see
> `vl53l7cx-frequency-and-integration.md` and `tof-min-max-distance.md`.
> Recovered from the planning session's transcript: the session reused one
> plan file for all its plans, so only the last one was left on disk.

## Context

The request:

> The thunder sensor board contains a vl53l7cx ToF sensor. Both the vl53l7cx
> datasheet and um3038 user guide can be found under the newly created docs/
> folder. The ULD driver source code has been downloaded into the
> src/internal/vl53l7cx/VL53L7CX_ULD_driver_2.0.1 directory. Chapter 4.13 of
> um3038 mentions the option of triggering an interrupt when motion is
> detected. We want to use this motion indicator feature of the vl53l7cx in
> autonomous ranging mode. For staters, let's use the default settings: 4x4,
> a 5ms integration time and 1Hz sampling frequency. Analyze the best way to
> implement this feature into our nrf code base and propose an
> implementation plan. The pin mapping of the vl53l7cx to the nrf52840 is as
> follows:
> * SCL and SDA ->  I2C1 (shared with sht20)
> * ToF.int -> SCK/P0.14
> * ToF.LPn -> MOSI/P0.13
> * ToF.RST -> MISO/P0.15
>
> We want this motion event to map to a DPT 1.018 (G) / DPT_Occupancy
> message with a configurable group address. Turn the Occupancy back off
> after a configurable timer in secs expires (no more motion interrupts
> happened during this interval).

## Feasibility, from the sensor docs and our code

- **Interrupt only on motion.** Chapter 4.13 of um3038 recommends combining
  the motion indicator with "detection thresholds" (chapter 4.12). With that,
  the INT pin (pin A3) only pulses when a zone's motion value exceeds a
  threshold. Example 11 in the driver does exactly this.
- **Nothing to read or clear.** The pulse is about 100 µs and clears itself.
  We don't need to read ranging data or acknowledge anything to keep it
  working (um3038 §5.3).
- **The pulse is too short to poll.** At 1 Hz frames that's at most one pulse
  a second while someone moves, so the nRF needs a pin interrupt to catch it.
- **The interrupt can't use I2C.** It only sets a flag; a goroutine picks the
  flag up.
- **Board wiring is already right.** The sensor board has 47 kΩ pull-ups on
  INT and LPn, and a 47 kΩ pull-down on RST. So the nRF drives LPn high, holds
  RST low, and reads INT as an input.
  - The sensor's I2C address is 0x29: the 0x52 in the schematic is the 8-bit
    form.
  - SCK/MOSI/MISO (P0.14/P0.13/P0.15) aren't used by anything else; the flash
    is on its own pins.
- **Power and heat.** Autonomous mode at 4x4, 1 Hz uses about 8 mW, according
  to datasheet Table 16. That table assumes 20 ms integration, so 5 ms should
  be lower still. Continuous mode is about 313 mW. Running it in autonomous
  mode should also reduce the heating you saw on the SHT20.

## Why port to Go rather than compile ST's C

- `CLAUDE.md` asks us to avoid cgo.
- We only need a small part of the C driver:
  - init, which includes uploading about 84 KB of sensor firmware;
  - resolution, frequency, ranging mode and integration time;
  - start and stop ranging;
  - the motion indicator plugin;
  - the detection thresholds plugin.

  That is roughly 500 lines of Go. Calibration (xtalk), power modes, sharpener
  and the other options are out of scope.
- **Firmware storage.** A generator extracts the firmware, default
  configuration, default crosstalk table and NVM command arrays from
  `vl53l7cx_buffers.h` into `.bin` files. Those are embedded as Go strings, so
  they stay in flash and cost no RAM.
  - Flash grows from 140 KB to about 230 KB, out of 1 MB.
- **Upload in chunks.** TinyGo's nRF I2C writes byte by byte from the CPU, so
  the firmware is copied 256 bytes at a time into a small buffer and written
  to successive register addresses. The goroutine yields between chunks, so
  the console and the bus keep running.
- **Upload time.** I2C1 runs at 100 kHz today, which would take about 8 s for
  the firmware. I'd move I2C1 to **400 kHz** for both chips; the SHT20
  supports it. The 4.7 kΩ pull-ups should be fine at that speed with short
  traces, but it's worth a scope check.
- **Sharing I2C1 with the SHT20 is safe.**
  - Each I2C transaction busy-waits without yielding, so the cooperative
    scheduler can't interleave two of them.
  - The SHT20 sleeps between its write and its read, so ToF transactions can
    safely run in that gap.
  - I2C1 is configured once in `main`, not in `runSensor`.

## Design

**`src/internal/vl53l7cx`** (portable Go, tested on the host)
- `Device{bus drivers.I2C; Address uint16; buf [1024]byte}`. The buffer is the
  C driver's temporary buffer.
- Methods:
  - `IsAlive`, `Init`;
  - `SetResolution(4x4)`, `SetRangingFrequency(1)`,
    `SetRangingMode(Autonomous)`, `SetIntegrationTime(5)`;
  - `MotionIndicatorInit(4x4)`, which keeps the default 400–1500 mm window;
  - `SetDetectionThresholds` and `EnableDetectionThresholds`;
  - `StartRanging`, `StopRanging`, and an optional `Motion()` that reads the
    per-zone motion values for debugging.
- Internals: the DCI read/write/replace helpers, the 32-bit byte swaps done
  explicitly in big-endian, and the polling helpers.
- **Thresholds:** 16 zones, each "motion greater than 44", using the example's
  value. um3038 quotes 100–500 for typical movement, so we'll tune this on the
  bench.
- **Main test: a recorded-transaction comparison.**
  1. Compile ST's C driver with gcc against a fake platform layer that logs
     every I2C transaction, with scripted replies for the status polls.
  2. Run init plus our exact configuration and save that log as test data.
  3. Run the Go port against the same fake replies and check its transactions
     match byte for byte.

  That catches porting mistakes in the offset/crosstalk 4x4 extrapolation, the
  byte swaps and the threshold encoding, all before anything touches the
  hardware.

**`thunder`**
- Add `Occupancy struct{ Group uint16; Timeout uint16 /* s */ }` to `Device`.

**`occupancy`** (a small portable state machine)
- `Motion(now)` returns "send ON" if not yet occupied, and always restarts the
  hold timer.
- `Poll(now)` returns "send OFF" once `Timeout` seconds have passed with no
  further motion.
- It's tested with fake timestamps.

**`bus`**
- A new `CmdMotion`. `bus.Run` owns the occupancy state machine because it
  already holds the current config (`cur`).
  - `config set occupancy.timeout` applies from the next motion.
  - An unbound group means motion is ignored.
- Its existing 10 ms ticker calls `Poll`.
- ON and OFF go through the existing write queue as
  `dpt.DPT_1018(true/false).Pack()`. They're gated on L_Data.con like every
  other telegram.

**`config`**
- An optional `[occupancy]` section with `group = <ga | ->` and `timeout =
  <seconds, 1..65535>`.
- New keys `occupancy.group` and `occupancy.timeout`, with an integer case
  added to `field`/`Get`/`Set`.
- `config dump` and `config init` pick both keys up automatically, and
  `default.ini` gains the section.

**`nrf/tof.go`** (TinyGo only)
- `runToF(cmds)`:
  1. Set LPn high and RST low.
  2. Set up the INT pin as an input with an interrupt on the falling edge; the
     interrupt only increments an atomic counter.
  3. Run `Init` and the configuration, then start ranging.
  4. Loop every 50 ms: if the counter moved, send `CmdMotion` without
     blocking.
- **Init failure:** if init fails, log `vl53l7cx: …` and retry every 60 s, the
  same pattern as the SHT20.
- **Console command `tof`:** shows the sensor state, the interrupt count, the
  time since the last motion and, optionally, the latest per-zone motion
  values (the readout from the driver's `Motion()`).

## Steps

1. Write the generator, the `.bin` files and the Go port.
2. Build the C fake-platform harness and record the reference log; add the
   transaction-comparison tests.
3. Add the occupancy state machine, then the thunder, config and bus changes,
   with tests.
4. Add `nrf/tof.go`, move I2C1 to 400 kHz and configure it in `main`, and add
   the `tof` command.
5. Run `make test` and `make nrf`, then check on hardware:
   - the firmware boots and the upload time is printed;
   - waving a hand produces an ON telegram on the group in `sniff`, with a
     positive L_Data.con;
   - after the timeout with no motion, an OFF telegram is sent;
   - SHT20 readings continue normally.

## Open decisions (as proposed)

1. **Default group and timeout for `default.ini`.** My suggestion is `0/2/1`
   and `300` s, following the 0/0/x switches and 0/1/x sensor pattern.
2. **What to commit from `src/internal/vl53l7cx/`.** I'd leave out the 3 MB
   `STSW-IMG036.zip` and the `CubeIDE_F401RE_Example` folder. I'd keep the ULD
   API sources, their `LICENSE.txt` and the generated `.bin` files. The API
   code is BSD-3, but the firmware blob's own licence terms are in the zip, so
   check that ST allows redistributing it in the repo.
3. **Motion threshold.** I'd start at 44 and make it a constant for now. It
   could become `occupancy.threshold` later if tuning shows it needs to be
   adjustable.

## Decisions (the user's reply)

- 1/ Default group 0/2/1 and default timeout set to 60 secs.
- 2/ I agree with your proposal
- 3/ Make the threshold adjustable from the get-go. Check any config setting
  against the allowed range. Default to 44.
