# Two configurable light switches driven from an interactive shell (host/FTDI)

> **Status: completed.** Implemented in commit `1cd0b0d` ("ini-configured
> switches with interactive shell") (2026-10-13). Recovered from the
> planning session's transcript: the session reused one plan file for all
> its plans, so only the last one was left on disk.

## Context

`src/ftdi/main.go` is a host prototype that sends one hard-coded "off"
`GroupValueWrite`
(src 7.8.1 → group 0/0/2) through the FTDI/NCN5120 adapter and then hex-dumps
RX bytes.
We want it to behave like a real two-channel switch:

1. Switches **A** and **B** get their individual and group address from an ini
   file at
   startup (parsed with `github.com/zieckey/goini`) into a proper data
   structure.
2. An interactive shell (`github.com/abiosoft/ishell`) accepts `switch A off`,
   `switch B on`, etc. and sends the matching KNX telegram, built and
   transmitted the way
   `ftdi/main.go` does today (`tp1.Telegram.Pack` → `ncn.TxData`).
3. The CLI runs in the main goroutine and hands commands over a buffered
   channel to a
   separate goroutine that transmits asynchronously.

The eventual target is nrf52840, so the switch logic stays free of host-only
dependencies;
only ini loading and the shell are host code.

## Design

```
main goroutine                         tx goroutine (owns the FTDI device)
ishell "switch A on"                   select {
  └─ parse args, look up switch        case cmd := <-cmds: Pack → print "> .." → ncn.TxData
  └─ cmds <- command{sw, on}  ───────► case <-ticker(10ms): d.Read → print "< .."
     (buffered, cap 16)                }
```

### New package `src/light/light.go` (portable: no os/fmt, usable from `src/nrf` later)

```go
type Switch struct {
    Name string
    Src  uint16 // individual address
    Dest uint16 // group address
}

// Telegram builds the GroupValueWrite for the requested state.
func (s *Switch) Telegram(on bool) tp1.Telegram
```

`Telegram` holds the struct literal currently duplicated in
`ftdi/main.go:43-56` and
`nrf/main.go:22-35` (PriorityLow, AddrGroup, Routing 0, UnnumberedDataPacket,
GroupValueWrite), with `Data: dpt.DPT_1001(on).Pack()`.

### New package `src/config/config.go` (host only)

```go
const NumSwitches = 2
type Config struct { Switches [NumSwitches]light.Switch } // index 0 = A, 1 = B

func Load(path string) (*Config, error)
func (c *Config) Switch(name string) *light.Switch // case-insensitive, nil if unknown
```

- `goini.New()` + `ParseFile(path)`, then `SectionGet("A"|"B",
  "individual"|"group")`.
  (Confirm the exact goini getter names against the module source after `go
  get`.)
- Addresses are parsed with the existing knx-go helpers
  `cemi.NewIndividualAddrString` and
  `cemi.NewGroupAddrString` (`knx/cemi/address.go`), so `7.8.1` and `0/0/2`
  notation work.
- Missing section/key or an unparsable address returns an error naming section
  and key;
  startup aborts via the existing `checkErr`.
- Fixed-size array with linear lookup, no maps (CLAUDE.md).

### New file `src/knixx.ini`

```ini
[A]
individual = 7.8.1
group      = 0/0/2

[B]
individual = 7.8.2
group      = 0/0/3
```

### `src/ftdi/main.go` (modified): setup + shell in the main goroutine

- `-config` flag (default `knixx.ini`); `config.Load` before opening the FTDI
  device.
- FTDI setup unchanged (lines 33-41).
- `cmds := make(chan command, 16)`; `go transmit(d, cmds, done)`.
- `shell := ishell.New()`; `shell.AddCmd(&ishell.Cmd{Name: "switch", Help:
  "switch <A|B> <on|off>", Func: ...})`.
  The handler calls `parseSwitch(c.Args)`, resolves `cfg.Switch(name)`, and
  enqueues with a
  non-blocking send (`select` + `default`): if the buffer is full it prints
  "tx queue full"
  instead of freezing the prompt. Bad arguments print the usage via
  `c.Println`.
- `shell.Run()` blocks; ishell supplies `help` and `exit`. On return:
  `close(cmds)`,
  wait on `done` so queued telegrams are flushed, then `d.Close()`.

### New `src/ftdi/tx.go`: transmission goroutine

```go
type command struct {
    sw *light.Switch
    on bool
}

func transmit(d io.ReadWriter, cmds <-chan command, done chan<- struct{})
```

- Sole owner of the device after startup: both `ncn.TxData` and the RX poll
  `d.Read` run
  here, because a libftdi context isn't safe for concurrent use. This moves
  the existing
  10 ms RX hex-dump loop (lines 63-69) into the same `select` as the command
  channel.
- Per command: `cmd.sw.Telegram(cmd.on).Pack()` → print `> % x` →
  `ncn.TxData`; errors are
  printed, the loop continues. Exits and signals `done` when `cmds` is closed
  and drained.
- Takes `io.ReadWriter` so tests can pass an in-memory fake instead of
  `*ftdi.Device`.
- RX/TX hex lines are printed from this goroutine and can land in the middle
  of the
  ishell prompt line; cosmetic only, accepted for the prototype.

### New `src/ftdi/shell.go`

`parseSwitch(args []string) (name string, on bool, err error)`: expects
exactly two args,
`A|B` and `on|off`, case-insensitive. Kept separate from the ishell handler
for testing.

### Small fix: `src/ncn/ncn.go`

`TxData` drops the `w.Write` error; return it so `transmit` can report a
failed send.

### Not touched

`src/main.go` (EEPROM tool), `src/nrf/main.go` (can adopt `light.Switch`
later), `tp1`.

## Files

| File | Change |
|---|---|
| `src/light/light.go`, `light_test.go` | new |
| `src/config/config.go`, `config_test.go` | new |
| `src/knixx.ini` | new |
| `src/ftdi/main.go` | config load, ishell, channel + goroutine start, shutdown |
| `src/ftdi/tx.go`, `tx_test.go` | new, tx/rx goroutine |
| `src/ftdi/shell.go`, `shell_test.go` | new, argument parser |
| `src/ncn/ncn.go` | propagate write error |
| `src/go.mod`, `go.sum` | `go get github.com/zieckey/goini github.com/abiosoft/ishell` |

## Verification

- `cd src && go test ./light ./config ./ftdi ./ncn ./tp1`
  - light: A-off with src 7.8.1 / group 0/0/2 packs to the same bytes the
    current prototype
    emits; on/off differ only in the data bit and checksum.
  - config: valid file → both switches populated; missing section, missing key
    and bad
    address each return an error.
  - shell: `A on`, `b OFF`, wrong arg count, unknown switch, unknown state.
  - tx: queue several commands against a fake `io.ReadWriter`, close the
    channel, and
    check all telegrams were written in order in NCN5120 framing before `done`
    fires.
- `go vet ./light ./config ./ftdi` and `go build ./ftdi` (needs libftdi
  headers).
- With the FTDI adapter attached: `go run ./ftdi -config knixx.ini`, then
  `switch A on`,
  `switch A off`, `switch B on`, `switch B off`, `exit`; check the `>` hex
  lines, the `<`
  bytes echoed by the NCN5120, and that the lights on the bus react. This step
  needs the
  hardware, so it's yours to run.
