# `config` CLI command: get and set the knixx.ini parameters

> **Status: completed.** Implemented in commit `b15fdbc` ("get and set
> knixx.ini parameters from the console") (2026-10-16). Recovered from the
> planning session's transcript: the session reused one plan file for all
> its plans, so only the last one was left on disk.

## Context

Today the addresses in `knixx.ini` (the individual address for `[device]`, the
group
address for `[A]` and `[B]`) are only read once at startup. On the Feather the
file sits in
littlefs on the external flash, and there's no way to change it short of
reformatting. We
want to view and change these parameters from the console on both builds (the
ftdi host
and the nrf target). A change should take effect immediately and survive a
restart.

## User-facing commands

```
>>> config
device.individual = 7.8.1
A.group = 0/0/2
B.group = 0/0/3
>>> config get A.group
0/0/2
>>> config set A.group 1/2/3
A.group = 1/2/3 (saved)
>>> config set device.individual 1.1.300
config: device.individual: <cemi parse error>
```

- Keys are `section.key`, mirroring the ini file. That's three keys today, and
  the
  pattern extends naturally if more parameters are added later. Section names
  match
  case-insensitively, as `switch` already does.
- `set` validates the value with the existing `cemi.NewIndividualAddrString` /
  `cemi.NewGroupAddrString`. A bad value changes nothing.
- A valid `set` updates the running device and then writes the whole file
  back. No separate
  `save` step: there are only three values, and the write is a few hundred
  bytes on
  littlefs or the host disk. If the write fails, the change stays live but is
  reported as
  `(not saved: <err>)`.

## Design

### `config` package (shared)

- `var Keys = [...]string{"device.individual", "A.group", "B.group"}`: the
  display order
  for `config` with no arguments.
- `Get(dev *light.Device, key string) (string, error)`, which formats with
  `cemi.IndividualAddr(...).String()` / `cemi.GroupAddr(...).String()`.
- `Set(dev *light.Device, key, value string) error`, which parses, validates
  and assigns.
- `Format(dev *light.Device) []byte` writes the ini back in a fixed order
  (`[device]`,
  `[A]`, `[B]`), byte-for-byte the same layout as `config/default.ini`. I'm
  not using
  goini's `Write`, because it walks maps and so produces a random section
  order. Comments
  aren't preserved; the file has none.
- `type Store interface { Save(data []byte) error }`. A `FileStore{Path
  string}` (host,
  in `load.go`, `!tinygo`) uses `os.WriteFile`.
- Test: `Parse(Format(dev))` round-trips to the same device.

### Data race: hand the new config to the transmit goroutine

`bus.Run` reads `dev` to build telegrams on another goroutine. On the host
that's a real
thread, so writing `dev` from the shell would race. TinyGo's cooperative
scheduler is
fine either way, but the shared code should be correct on both.

- `bus.Command.Sw *light.Switch` becomes `Switch uint8` (an index). Pointers
  into the
  shell's copy would otherwise leak into the bus goroutine.
- `bus.Run` makes its own copy of `*dev` at start.
- New `bus.CmdConfig` carries `Dev light.Device` by value. `Run` replaces its
  copy, so
  telegrams queued before the `set` use the old addresses and later ones use
  the new.
- `light.Device` gets `SwitchIndex(name) (uint8, bool)`; `Switch(name)` stays.

### `cli` package

- `Commands(dev *light.Device, store config.Store, cmds chan<- bus.Command)`
  adds `config`:
  - no arguments: list `Keys` with `Get`;
  - `get <key>`;
  - `set <key> <value>`: `config.Set`, then `enqueue(CmdConfig{Dev: *dev})`,
    then
    `store.Save(config.Format(dev))`.

  Usage errors print `usage: config [get <key> | set <key> <value>]`. `switch`
  enqueues
  the index from `dev.SwitchIndex`.

### Wiring

- `ftdi/main.go`: pass `config.FileStore{Path: *configPath}`.
- `nrf/storage.go`: generalise `writeDefault` into `writeConfig(data []byte)
  error`
  (`OpenFile` with `O_CREATE|O_WRONLY|O_TRUNC`, then `Write`), and a
  `flashStore struct{}` whose `Save` calls it.
  - If the flash or littlefs failed at boot (the built-in default fallback),
    `Save`
    returns `flash unavailable`. The change is then live but not persisted,
    and says so.

## Files

| File | Change |
|---|---|
| `src/config/config.go` | `Keys`, `Get`, `Set`, `Format`, `Store` |
| `src/config/load.go` | `FileStore` |
| `src/config/config_test.go` | Get/Set/validation/round-trip tests |
| `src/light/light.go` | `SwitchIndex` |
| `src/bus/bus.go`, `bus_test.go` | switch index, own device copy, `CmdConfig` |
| `src/cli/cli.go`, `cli_test.go` | `config` command; store parameter; a fake store in tests |
| `src/ftdi/main.go`, `src/nrf/main.go`, `src/nrf/storage.go` | pass the store; `writeConfig` |

## Verification

- `cd src && go vet ./bus ./cli ./config ./ftdi ./light ./ncn && go test
  ./...`
  - cli: `config`, `config get a.group`, `config set B.group 1/2/3`. The last
    one leaves
    `CmdConfig` queued, and the fake store receives the formatted ini. Bad
    key, bad value
    and wrong argument count leave both untouched.
  - bus: queue `CmdSwitch`, `CmdConfig` (with a new group), `CmdSwitch`. The
    two
    telegrams carry the old and new destination respectively.
- `tinygo build -target=feather-nrf52840 ./nrf` succeeds.
- Host: run the binary with `-config` pointing at a scratch copy of
  `knixx.ini`, do
  `config set A.group 0/0/9`, `exit`, and check the file changed.
- Feather, yours to run: `config set A.group …`, then reset the board;
  `config` shows the
  new value. `sniff on`, then `switch A on` shows the new destination in the
  `>` line.
