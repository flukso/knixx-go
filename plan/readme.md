# Add a README.md

> **Status: completed.** Implemented in commit `488a8ac` ("add a README and
> the MIT and CERN-OHL-P licences") (2026-10-18). Recovered from the
> planning session's transcript: the session reused one plan file for all
> its plans, so only the last one was left on disk.

## Context

The repo has no README. The user wants one that at minimum covers:
1. what the device does;
2. the code layout, top-level constructs and flow;
3. every configuration parameter, with its defaults, limits and dependencies.

The facts below are taken from the code as of commit 05e7bc2. Write a single
file,
`/README.md`, in English, with GitHub-flavoured Markdown and ASCII diagrams so
it renders
anywhere. Don't change any code.

## Outline

### 1. knixx thunder (intro)
A KNX TP1 device built on an Adafruit Feather nRF52840 Express and the knixx
Thunder and
Thunder Sensor boards (schematics in `pcb/`). It is written in Go and built
with TinyGo. It
talks to the KNX bus through an onsemi NCN5121 transceiver on UART0 at 38400
baud.

### 2. Functionality
- **Two switches, A and B**, driven from the console with `switch A on|off`.
  Each sends DPT
  1.001 to up to 8 group addresses, one telegram at a time, each waiting for
  its
  L_Data.con (with a 500 ms timeout).
- **Temperature and humidity** from an SHT20 on I2C1:
  - read every 60 s, the first reading 5 s after boot;
  - published as DPT 9.001 (°C) and DPT 9.007 (%).
- **Occupancy** from the motion indicator of a VL53L7CX time-of-flight sensor:
  - autonomous ranging at 4x4, with detection thresholds;
  - INT pulses on frames with motion, and the first one sends DPT 1.018 ON;
  - OFF follows after the timeout without motion.
- **Configuration:**
  - stored as `knixx.ini` on a littlefs on the external QSPI flash;
  - edited live from the console; changes are saved and applied without a
    reboot;
  - falls back to the built-in `default.ini`.
- **Console** on USB CDC:
  - commands: `help`, `switch`, `sniff`, `stat`, `config`, `sensor` and `tof`,
    each with
    its usage line;
  - line editing with history;
  - tip: use `screen`, not `tinygo monitor`, which garbles pasted input.
- **Host tool `src/ftdi`:** the same shell and bus logic on a PC, using an
  FTDI USB-serial
  adapter wired to the NCN5121 and a local `src/knixx.ini`.

### 3. Building and testing
- **Requirements:** TinyGo 0.42.0 at `/usr/local/lib/tinygo`, and Go for the
  host tests.
- **Targets:**
  - `make nrf` builds `build/knixx.uf2`;
  - `make flash`;
  - `make test` runs `go vet` and `go test ./...` on the host.
- **Patched TinyGo root:** explain `patches/` and `tinygo/root-0.42.0`. There
  are two USBD
  fixes (epdata, single DMA), applied automatically by the Makefile.
- **Regeneration:** `go generate ./internal/vl53l7cx` regenerates the firmware
  blobs and the
  golden log. The golden log needs `cc`.

### 4. Code layout
A table of each package under `src/`, with its role:

| package | role |
|---|---|
| `nrf` (tinygo build tag) | `main`; the pins; the goroutines for console, SHT20 and ToF; littlefs storage |
| `ftdi` | host `main` |
| `bus` | the NCN5121 transmit/receive loop `Run`, `Command` kinds, occupancy timer |
| `cli` | the commands shared by both mains, plus `Exec` |
| `config` | ini `Parse`/`Format`/`Get`/`Set`, `Keys`, the embedded `default.ini`, `FileStore` |
| `thunder` | the device model: `Device`, `Switch`, `Sensor`, `Occupancy`, limits, telegram building |
| `tp1` | TP1 telegram packing |
| `ncn` | NCN5121 services, `TxData`, the indication `Parser` |
| `lineedit` | the console line editor |
| `internal/sht2x` | SHT20 driver |
| `internal/vl53l7cx` | Go port of ST's ULD 2.0.1: embedded firmware, motion indicator, thresholds |

Plus where the C ULD sources, the harness, the golden test and `genfw` live.

### 5. Top-level flow
An ASCII diagram of the goroutines and the `cmds` channel (QueueLen 16):

```
 console (USB CDC) ──cli.Exec──┐
 runSensor  (SHT20, 60 s) ─────┼──► cmds ──► bus.Run ──► NCN5121 (UART0) ──► KNX
 runToF (VL53L7CX INT) ─CmdMotion┘          ▲ L_Data.con / indications
```

Then the steps:
1. **Boot** (`nrf/main.go`):
   - UART0;
   - `loadConfig`: mount the littlefs (formatting if needed), write the
     default when the
     file is missing, parse it, or fall back to the built-in default;
   - I2C1 at 400 kHz;
   - start the goroutines.
2. **`bus.Run`:**
   - owns the UART and a copy of the config;
   - one command at a time, with queued writes each confirmed by L_Data.con;
   - `CmdConfig` swaps in a new config;
   - the occupancy timer runs on the 10 ms tick.
3. **`runToF`:**
   - reset, boot (firmware upload of about 2 s), configure, start;
   - poll INT pulses every 50 ms and queue `CmdMotion`;
   - apply setting changes with stop, reprogram, start;
   - retry after 60 s on error.
4. **Config changes:** `config set` validates the value, then sends
   `CmdConfig` to bus.Run,
   then saves `Format(dev)`. The ToF goroutine picks up its settings by
   polling.
   - A note on the cooperative single-core scheduler: why sharing `dev` and
     I2C is safe.

### 6. Configuration
- **The file:** the default `knixx.ini` is shown verbatim, from
  `src/config/default.ini`.
- **Two kinds of default:**
  - the `default.ini` values, written when no file exists and restored by
    `config init`;
  - the value used when a key is missing from an existing file. This matters
    for the
    optional sections; for example, `occupancy.group` stays unbound.
- **Parameter table:** for each key, the type, the `default.ini` value, the
  value when the
  key is missing, and the limits:
  - **`device.individual`:** required. An individual address a.b.c (area 0–15,
    line 0–15,
    device 0–255); `default.ini` 7.8.1.
  - **`A.group` and `B.group`:** required. 1–8 comma-separated group
    addresses, no
    duplicates; `default.ini` 0/0/1 and 0/0/2.
  - **`temperature.group` and `humidity.group`:** optional. A single group
    address or `-`;
    0/0/0 is rejected. `default.ini` 0/1/1 and 0/1/2; unbound when missing.
  - **`occupancy.group`:** optional, the same format. `default.ini` 0/2/1;
    unbound when
    missing.
  - **`occupancy.timeout`:** 1–65535 s, default 60.
  - **`occupancy.threshold`:** 1–32767, default 44.
  - **`occupancy.frequency`:** 1–60 Hz, default 1.
  - **`occupancy.integration`:** 2–1000 ms, default 5.
  - **`occupancy.min_distance` and `occupancy.max_distance`:** 400–4000 mm
    each, defaults
    400 and 1500.
- **Dependencies and caveats:**
  - **Frame period:** integration + 1 ms must be shorter than 1000/frequency
    ms. Otherwise
    the sensor lowers the frame rate, and `tof` warns.
  - **Distance window:** min < max and max − min ≤ 1500 mm. An invalid pair is
    accepted by
    the config, so keys can be set in any order, but the ToF keeps the last
    valid window and
    `tof` warns.
  - **Threshold sensitivity:** motion values depend on integration time,
    distance and
    reflectance (ST says typical motion gives 100–500), so retune `threshold`
    with the
    per-zone values `tof` shows.
  - **Occupancy needs `occupancy.group`:** with it unbound, motion is ignored
    and nothing
    is sent.
  - **File size:** `knixx.ini` must stay under 1024 bytes on the nRF
    (`configBuf`).
  - **Console lines:** at most 128 characters (`lineedit.MaxLine`).
  - **Key names:** case-insensitive in `config get` and `config set`.
  - **Unknown content:** unknown sections and keys are ignored. That includes
    the old
    `[sensor]` section, which earlier versions used.
- **Console usage:**
  - `config` lists the keys;
  - `config get` and `config set`;
  - `config dump` prints set commands which, replayed, reproduce the file
    exactly;
  - `config init` restores the default.

### 7. Hardware pins
A short table:
- **NCN5121:** UART0.
- **I2C1:** SDA/SCL, shared by the SHT20 and the VL53L7CX.
- **VL53L7CX:** INT on P0.14 (SCK), LPn on P0.13 (MOSI), I2C_RST on P0.15
  (MISO).
- **QSPI flash:** on SPI0, in single-line mode (P0.17/22/19/20), with WP# and
  HOLD# on
  P0.23/P0.21.

### 8. Licences / third-party
ST's ULD is under its own licence, in
`VL53L7CX_ULD_driver_2.0.1/.../LICENSE.txt`. The knx-go,
goini, tinygo drivers and tinyfs dependencies are listed in go.mod. Don't
state a licence for
the project itself (there is none in the repo); leave a note for the user
instead.

## Facts to double-check while writing

- Whether `parseGroups` rejects 0/0/0 for the switches; it isn't checked
  today.
- The exact limits `cemi.NewIndividualAddrString` and `NewGroupAddrString`
  accept (3-level
  group 0–31/0–7/0–255).
- The `sniff` and `stat` behaviour, from `cli.go`.
- Whether the ftdi tool's `knixx.ini` path flag defaults to `knixx.ini`; it
  does.

## Verification

- Every key in `config.Keys` appears in the table.
- Each limit matches the `thunder` constants and the `config.field` cases.
- The default block is byte-identical to `src/config/default.ini`.
- Each command's usage line matches `cli.go`, `nrf/sensor.go` and
  `nrf/tof.go`.
- `make test` is unaffected, since the change is docs only.
