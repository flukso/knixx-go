# Rename sensor.temperature / sensor.humidity to temperature.group / humidity.group

> **Status: completed.** Implemented in commit `05e7bc2` ("move the sensor
> groups to [temperature] and [humidity]") (2026-10-18). Recovered from the
> planning session's transcript: the session reused one plan file for all
> its plans, so only the last one was left on disk.

## Context

Every other bindable thing in `knixx.ini` has its own section with a `group`
key: `[A]`, `[B]`
and `[occupancy]`. The SHT20 values instead sit together under `[sensor]` as
`temperature =`
and `humidity =`. Renaming them to `[temperature] group =` and `[humidity]
group =` makes the
layout consistent. It also leaves room for per-value settings later, such as a
send interval
or a change threshold.

**Decision (user): clean break.** `[sensor]` is no longer read. A board with
an old file stops
publishing temperature and humidity until the user runs `config init` or sets
the two new keys.

## New layout

```ini
[temperature]
group = 0/1/1

[humidity]
group = 0/1/2
```

- **Keys:** `temperature.group` and `humidity.group`. Each takes a single
  group address, or
  `-` to unbind, exactly as now.
- **Optional sections:** both are optional, and a missing section leaves its
  value unbound.
- **Format:** writes each section only when its group is bound, so it is
  omitted individually.
  Today `[sensor]` is omitted when both values are unbound.

## Changes

Scope is the config layer only. `thunder.Sensor{Temperature, Humidity}` and
its users in
`src/bus/bus.go` and `src/nrf/sensor.go` stay as they are; the struct
describes the hardware
and is not part of the file layout.

1. **`src/config/config.go`**
   - **Constants:** drop `sensorSection`, `temperatureKey` and `humidityKey`;
     add
     `temperatureSection = "temperature"` and `humiditySection = "humidity"`.
     The key is the
     existing `groupKey`.
   - **Parse:** the optional-sensor loop iterates `{section, *uint16}` pairs
     and reads
     `ini.SectionGet(section, groupKey)`. Errors become `config: [temperature]
     group: …`.
   - **Keys:** `"temperature.group", "humidity.group"`, kept in the same
     position (file order).
   - **field:** two cases matching `temperatureSection` and `humiditySection`
     with `groupKey`,
     returning `ref{group: …}`.
     - **Order matters:** these cases must come before the
       `dev.Switch(section)` fallback.
       That fallback only matches the switch names `A` and `B`, so there's no
       clash, but the
       explicit cases go first anyway.
   - **Format:** replace the `[sensor]` block with one block per bound value,
     each written as
     `\n[%s]\n%s = %s\n`.
   - **Set doc comment:** "a sensor key" becomes "temperature.group,
     humidity.group".
2. **`src/config/default.ini`**: replace the `[sensor]` block with the two
   sections. The
   `TestFormatMatchesDefault` test enforces that it matches Format byte for
   byte.
3. **Tests**
   - `src/config/config_test.go` `TestSensor`: switch to the new keys and
     sections, and add:
     - only `[humidity]` bound → Format writes only `[humidity]`;
     - a legacy `[sensor]` section is ignored: values stay unbound, and Parse
       doesn't fail,
       because unknown sections are already ignored.
   - `src/cli/cli_test.go`: the `sensor.humidity` keys become
     `humidity.group`.
4. **Comments:** check whether package docs in config.go and thunder.go
   mention `[sensor]`,
   and update them.

## Verification

1. `make test` passes, including the round trips, the default.ini match, and
   `config dump`
   replay in cli_test.
2. `make nrf` builds.
3. On the board, after flashing:
   - Run `config init`, or `config set temperature.group 0/1/1` and
     `config set humidity.group 0/1/2`.
   - `config dump` lists `temperature.group` and `humidity.group`.
   - The SHT20 telegrams appear again under `sniff on`.
