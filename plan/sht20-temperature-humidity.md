# Publish SHT20 temperature and humidity on the KNX bus

> **Status: completed.** Implemented in commit `239ac4e` ("publish SHT20
> temperature and humidity on KNX") (2026-10-17). This is the approved
> revision, after three rounds of feedback: "Make sure that the cyclic
> reading of temp and humidity happen at exact 60sec intervals. I'm worried
> this will not be the case with a sleep() construct.", "Why not use
> time.Ticker which is more idiomatic go?" and "The FTDI setup uses the same
> knixx main module and sensor board. So the sensor code should be included
> in the FTDI build as well." Recovered from the planning session's
> transcript: the session reused one plan file for all its plans, so only
> the last one was left on disk.

## Context

The Thunder Sensor board has an SHT20 (U2, I2C address 0x40, 1000000b) on
SDA/SCL. On the
Thunder board those lines go to the Feather's SDA/SCL pins (D22/D23 =
`machine.SDA_PIN`/
`SCL_PIN`). The goal is to read temperature and humidity every minute and
publish them as
KNX GroupValueWrite telegrams.

**Is there a driver?** No. tinygo.org/x/drivers v0.36.0, the latest release
and the one in
`go.mod`, has `sht3x`, `sht4x`, `shtc3` and `aht20`, but no `sht2x`. I checked
both the
module cache and the upstream repo listing. The SHT3x/4x drivers use a
different command
set (16-bit commands, a combined T+RH read), so they won't work with an SHT20.
The SHT2x
protocol is small, so I'll write a driver in the drivers style: it takes
`drivers.I2C`,
returns milli°C and hundredths of a percent, and lives in `src/internal/sht2x`
as CLAUDE.md
asks.

**PDU:** an A_GroupValue_Write with a 2-byte float.
- Temperature uses **DPT 9.001** (°C) and humidity uses **DPT 9.007** (%).
- knx-go already packs these: `dpt.DPT_9001(v).Pack()` and
  `dpt.DPT_9007(v).Pack()` return
  `{0, hi, lo}`. `tp1.Telegram.Pack` merges the leading 0 into the APCI octet,
  the same way
  `DPT_1001` works today, so the NPCI length becomes 3.

**Group addresses** (your choice): a new `[sensor]` section, with `temperature
= 0/1/1`
and `humidity = 0/1/2` in `default.ini`. The section is optional when parsing,
so the
`knixx.ini` already on your flash (which has no `[sensor]`) still loads. In
that case
nothing is sent until you run `config set sensor.temperature …`.

## Hardware gotcha: use I2C1, not I2C0

On the nRF52840, TWIM0 and SPIM0 share one peripheral instance (0x40003000).
`SPI0`
already drives the external flash, so `machine.I2C0` would clash with the
littlefs. Use
**`machine.I2C1`** (TWIM1) on `SDA_PIN`/`SCL_PIN` at 100 kHz. The board
already has 4K7
pull-ups (R2/R3).

## Design

### `src/internal/sht2x` (new, portable, host-tested)

```go
const Address = 0x40
type Device struct { bus drivers.I2C; Address uint16 }
func New(bus drivers.I2C) Device
func (d *Device) Configure() error                  // soft reset 0xFE, wait 15 ms
func (d *Device) ReadTemperature() (int32, error)   // milli°C
func (d *Device) ReadHumidity() (int32, error)      // hundredths of %RH, clamped 0..10000
```

- A read uses no-hold master mode, so the bus stays free while the sensor
  measures:
  1. Write `0xF3` (T) or `0xF5` (RH).
  2. Sleep for the datasheet maximum: 85 ms for 14-bit T, 29 ms for 12-bit RH.
  3. Read 3 bytes (MSB, LSB, CRC).
- **CRC-8:** polynomial 0x31 (x⁸+x⁵+x⁴+1), initial value 0. On a mismatch it
  returns
  `ErrChecksum`.
- The two status bits are masked with `raw &^ 3`.
- Integer conversion:
  - T = −46850 + 175720·raw/65536
  - RH = −600 + 12500·raw/65536
  - Both are computed in int64 so they don't overflow, with no floats in the
    driver.
- `drivers.I2C` is only an interface, so importing it builds on the host too.
  `*machine.I2C` satisfies it.

### `light` (device model)

- Add `Sensor struct { Temperature, Humidity uint16 }` to `Device`. A value of
  0 means
  unbound. Group 0/0/0 is the broadcast address and never a valid binding.
- Add `func (d *Device) GroupWrite(group uint16, data []byte) tp1.Telegram`,
  which holds
  today's header fields. `Telegram(group, on)` becomes
  `GroupWrite(group, dpt.DPT_1001(on).Pack())`, so the golden-byte tests still
  pass.

### `config`

- `Parse`: if the `[sensor]` section or its keys are missing, they stay
  unbound and this is
  not an error. A present key must be a single valid group address.
- `Keys` gains `sensor.temperature` and `sensor.humidity`. `field()` is
  extended to return a
  `*uint16` for these two keys, which reuses the same path as the individual
  address but
  parses with `cemi.NewGroupAddrString`.
- `Get` prints the address, or `-` when the key is unbound. `config set
  sensor.temperature
  -` unbinds it.
- `Format` writes a `[sensor]` section with the keys that are bound, and omits
  the section
  when neither is bound.
- `default.ini` gains the `[sensor]` section with 0/1/1 and 0/1/2.
  `TestFormatMatchesDefault` keeps the default round-tripping byte for byte.

### `bus`: a generic send queue, plus `CmdSensor`

- Add a new `CmdSensor` kind, with `Temperature int32` (m°C) and `Humidity
  int32` (c%) on
  `Command`. Run resolves the groups from its own copy `cur`. The sensor
  goroutine therefore
  never reads the `dev` that the CLI changes, and `config set` takes effect
  through the
  existing `CmdConfig`.
- Generalise the pending state in `Run`: replace `sw`/`on`/`next` with a fixed
  array of
  outgoing writes:
  ```go
  type write struct { group uint16; data [3]byte; n uint8 }
  var queue [light.MaxGroups]write; var nq, next uint8
  ```
  - `CmdSwitch` fills one write per bound group, using `DPT_1001`.
  - `CmdSensor` fills up to two writes for the bound groups, using
    `DPT_9001(float32(t)/1000)`
    and `DPT_9007(float32(h)/100)`.
  - The L_Data.con gating, the timeout messages and nil-channel blocking are
    unchanged.
- `send(d, dev, group, data []byte)` calls `dev.GroupWrite`.

### `src/nrf/sensor.go` (new, tinygo)

```go
const sensorInterval = time.Minute
func runSensor(cmds chan<- bus.Command)
```

- **Setup:**
  - Configure `machine.I2C1` (SDA_PIN, SCL_PIN, 100 kHz).
  - Call `sht2x.New(machine.I2C1)` and `Configure()`.
  - If configuration fails, it logs `sht20: …` to `console` and retries on
    every tick, so a
    board without the sensor keeps working.
- **Timing: a `time.Ticker`, not `time.Sleep(time.Minute)`.**
  - A plain `measure(); Sleep(60 s)` loop would drift. Each cycle would last
    60 s plus the
    measurement (about 115 ms) plus scheduler latency, so the error would grow
    every
    minute.
  - A ticker doesn't drift. I checked this in TinyGo 0.42's cooperative
    scheduler:
    `reAddTimer` re-arms with `when += period`
    (`runtime/scheduler_cooperative.go:161`).
    Tick k is therefore due at `t0 + k·60 s`, measured from when the ticker
    was created.
    It is not computed from the moment the previous tick was handled.
    - The measurement time and any lateness in receiving a tick don't carry
      over to the
      next tick.
    - If the goroutine ever misses a whole period, the channel's 1-slot buffer
      drops the
      extra ticks, so readings never burst.
    - This matches Go's own ticker semantics.
  - Shape:
    ```go
    time.Sleep(firstDelay)               // ~5 s after boot
    measureAndPublish()
    ticker := time.NewTicker(sensorInterval)
    for range ticker.C { measureAndPublish() }
    ```
  - Wake-up jitter per tick is set by the cooperative scheduler. The other
    goroutines
    yield every 10 ms: the console poll and the bus `rxPoll`. The worst case
    is a
    `config set` writing to the flash at the same moment, which takes tens of
    milliseconds.
    That jitter never accumulates.
  - The sample point is the moment T is triggered. Fixed delays come after it:
    the T and RH
    conversion (85 + 29 ms) and the bus send. They are the same every cycle,
    so the
    interval between publications is still 60 s.
  - The long-term rate follows TinyGo's monotonic clock on the nRF52840. That
    clock is RTC0
    on the 32.768 kHz LFCLK, sourced from the Feather's crystal (about ±20
    ppm), so it is
    off by at most about 1.7 s per day against wall-clock time. There is no
    RTC/NTP sync,
    so samples line up to boot time, not to the wall-clock minute.
- **Loop:**
  - On each tick it reads T and then RH. It stores the latest reading in
    package variables
    for the `sensor` command, then enqueues `CmdSensor` without blocking. A
    full queue
    prints `tx queue full`.
  - The first reading happens about 5 s after boot, then every 60 s from the
    ticker.
- **Errors:** a read error, such as a NACK or `ErrChecksum`, is printed once
  per tick and
  that cycle is skipped.

`main.go` starts it with `go runSensor(cmds)`. It appends one nrf-only console
command to
`cli.Commands(...)`:
- `sensor` prints the last reading and its age, for example
  `temperature 21.53 °C, humidity 48.20 %RH (12 s ago)`, or `no reading yet`.

The ftdi build has no sensor and doesn't change, apart from the shared
`config`/`light`
changes. Its `config` command also shows the sensor keys.

## Files

| File | Change |
|---|---|
| `src/internal/sht2x/sht2x.go`, `sht2x_test.go` | new driver, plus tests with a fake I2C |
| `src/light/light.go`, `light_test.go` | `Sensor`, `GroupWrite`; DPT 9 golden test |
| `src/config/config.go`, `default.ini`, `config_test.go` | optional `[sensor]` section, keys, `-` unbind |
| `src/bus/bus.go`, `bus_test.go` | write queue, `CmdSensor` |
| `src/cli/cli.go` | usage text only (if needed) |
| `src/nrf/sensor.go`, `src/nrf/main.go` | goroutine on I2C1, `sensor` command |
| `src/go.mod` | no new module; `tinygo.org/x/drivers` is already required |

## Verification

- `cd src && go vet ./... && go test ./...`, covering:
  - **sht2x:**
    - the datasheet CRC example (`0x68 0x3A → 0x7C`)
    - conversion of known raw values: 0x6614 gives about 23.4 °C; 0x7C80 gives
      about 54.8 %
    - status-bit masking, a checksum error, and the command bytes written
  - **light:** `GroupWrite` golden bytes for 21.5 °C (DPT 9 0x0C33) to 0/1/1:
    `bc 78 01 08 01 e3 00 80 0c 33 <cs>`
  - **config:**
    - a file without `[sensor]` parses as unbound
    - get/set/unbind, plus rejecting a group list for a sensor key
    - the round trip, and `default.ini` round-tripping
  - **bus:**
    - `CmdSensor` sends the temperature frame, then the humidity frame, gated
      on
      L_Data.con
    - an unbound key is skipped
    - existing switch tests still pass
- `tinygo build -target=feather-nrf52840 ./nrf` succeeds; I'll compare the
  flash and RAM
  size with the current 131,140 B.
- Hardware (yours):
  1. Run `sensor` about 5 s after boot to see plausible values. The command
     also prints
     the uptime at which the reading was triggered, in ms. Repeat it over a
     few minutes and
     the uptimes should be exactly 60 000 ms apart, give or take a few ms. A
     bus monitor's
     timestamps show the same spacing.
  2. Turn `sniff on` and wait up to a minute: two `>` frames should appear,
     each with
     `L_Data.con: positive`.
  3. Check them in ETS or another bus monitor as DPT 9.001/9.007 on 0/1/1 and
     0/1/2.
  4. Your existing flash `knixx.ini` lacks `[sensor]`, so run
     `config set sensor.temperature 0/1/1` and `config set sensor.humidity
     0/1/2` first.
