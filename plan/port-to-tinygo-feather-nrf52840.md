# Port the knixx switch to TinyGo on the Feather nRF52840 Express

> **Status: completed.** Implemented in commit `e038247` ("port the switch
> to the Feather nRF52840") (2026-10-16). Recovered from the planning
> session's transcript: the session reused one plan file for all its plans,
> so only the last one was left on disk.

## Context

The working host prototype (`src/ftdi`: an ishell CLI, an ini config, a
transmit goroutine
and NCN5121 decoding over an FTDI adapter) must run on the nRF52840 Feather
instead:

- config lives in littlefs on the board's external flash (GD25Q16C);
- a default `knixx.ini` is written on first boot;
- the same commands (`switch`, `sniff`, `stat`) are reachable over USB CDC.

The code goes in `src/nrf`. Logic shared with `src/ftdi` moves into sibling
packages so
that both mains are thin.

Facts checked locally:
- TinyGo 0.42.0; the `feather-nrf52840` target already defaults to `serial:
  usb`, so
  `machine.Serial` is the USB CDC port.
- `machine.UART.Read` is non-blocking: it returns `0, nil` when empty, like
  the ftdi
  device, so the shared transmit loop works unchanged.
- `machine.SPI0` is a `*machine.SPI`.
- **ishell can't be used on the device.** Its readline dependency puts the OS
  terminal
  into raw mode via termios ioctls (`syscall`, `golang.org/x/sys/unix`), needs
  a tty file
  descriptor on `os.Stdin`, and pulls in `os/signal`, `os/exec` and `net`.
  None of these
  exist on bare-metal TinyGo; the console there is a USB CDC byte stream.

## Package layout (all under `src/`)

| Package | Status | Contents |
|---|---|---|
| `light`, `tp1`, `ncn` | unchanged | already free of OS dependencies |
| `config` | split | `config.go`: `Parse(data []byte) (*light.Device, error)` using `goini.New().Parse(data, "\n", "=")`, plus `//go:embed default.ini` exposed as `config.Default []byte`. `load.go` (`//go:build !tinygo`): `Load(path)` = `os.ReadFile` + `Parse` |
| `config/default.ini` | new | the same content as `src/knixx.ini` (the host keeps its file for `-config`) |
| `bus` | new, moved from `ftdi/tx.go` | `Command`, `CmdSwitch`/`CmdStat`, `Run(d io.ReadWriter, dev, cmds, done)` (was `transmit`), `receiver`, `stat`, `Sniff atomic.Bool`, `Out io.Writer`. Error prints go to `Out`, not `os.Stderr`. Tests move along (`ftdi/tx_test.go` → `bus/bus_test.go`) |
| `cli` | new, moved from `ftdi/shell.go` + the handlers in `ftdi/main.go` | `type Cmd struct{ Name, Help string; Func func(args []string, w io.Writer) }`; `Commands(dev, cmds chan<- bus.Command) []Cmd` builds `switch`, `sniff` and `stat` with the non-blocking `enqueue`; `Exec(cmds []Cmd, line string, w io.Writer) (quit bool)` splits fields, dispatches, and handles `help` and `exit`. `parseSwitch`/`parseOnOff` and their tests move here |
| `ftdi` | slimmed | FTDI setup, `config.Load`, `go bus.Run(...)`, and an ishell loop that registers each `cli.Cmd` with an adapter `func(c *ishell.Context) { cmd.Func(c.Args, os.Stdout) }`. Behaviour unchanged |
| `nrf` | rewritten | see below; every file has `//go:build tinygo`, so host `go test ./...` skips it |

The current `nrf/main.go` (one hard-coded telegram sent from a P0_07 pin
interrupt) is
replaced. The stale `nrf/*.elf` build outputs are left alone (untracked).

## `src/nrf`

### `storage.go`: external flash + littlefs

```go
blockDev := flash.NewSPI(machine.SPI0, machine.P0_17, machine.P0_22, machine.P0_19, machine.P0_20)
// SDO=DATA0, SDI=DATA1, SCK, CS as specified
```

- `blockDev.Configure(&flash.DeviceConfig{Identifier:
  flash.DefaultDeviceIdentifier})`
  (this identifies the GD25Q16C). If it fails, report it on the console and
  don't format.
- As a precaution, drive DATA2/WP# (P0_23) and DATA3/HOLD# (P0_21) high as
  GPIO
  outputs, so the chip isn't write-protected or held when its QE bit is clear.
- `fs := littlefs.New(blockDev)`; `fs.Configure(&littlefs.Config{CacheSize:
  512, LookaheadSize: 512, BlockCycles: 100})`
  (values from the tinyfs QSPI example).
- `loadConfig() (*light.Device, error)`:
  1. `fs.Mount()`. If it fails, there is no littlefs: `fs.Format()`, mount
     again,
     write `config.Default` to `/knixx.ini`, and print `formatted flash, wrote
     default knixx.ini`.
  2. If the mount succeeds but `/knixx.ini` is missing, write the default too,
     and say so.
  3. Read `/knixx.ini` into a fixed `[1024]byte` buffer and call
     `config.Parse`.
  4. If the file exists but doesn't parse, print the error and fall back to
     `config.Parse(config.Default)` without overwriting the file, so the
     device still
     comes up.

### `console.go`: line shell over USB CDC

- Reads from `machine.Serial` byte by byte with `Buffered()`/`ReadByte()`, and
  sleeps
  10 ms when there is no input (this yields to the cooperative scheduler).
- Echoes input and handles CR/LF and backspace/DEL. The line buffer is a fixed
  `[80]byte`. Prompt `>>> `. Each complete line goes to `cli.Exec(cmds, line,
  machine.Serial)`.

### `main.go`

- `machine.UART0.Configure(machine.UARTConfig{BaudRate: 38400})` on the
  board's default
  UART pins, as the existing nrf prototype does.
- `bus.Out = machine.Serial`; sniff stays off by default.
- `dev := loadConfig()`; `cmds := make(chan bus.Command, 16)`; `go
  bus.Run(machine.UART0, dev, cmds, done)`.
- Run the console loop forever (no `exit` on the device: `Exec` returns quit,
  which
  just reprints the prompt).

## Dependencies

`cd src && go get tinygo.org/x/drivers@v0.36.0 tinygo.org/x/tinyfs@v0.5.0`
(latest tags).
I'll check the `flash.NewSPI`, `littlefs.New/Configure/Mount/Format/OpenFile`
signatures
in those versions before writing the code. littlefs is C code compiled by
TinyGo itself.
That is the one cgo exception to the CLAUDE.md rule, and comes from the
required library.

Risk: `goini` and the `cemi` address parser haven't been compiled with TinyGo
yet. Both
only use std packages TinyGo supports (`bufio`, `bytes`, `strconv`, `strings`,
`os`). The
first implementation step is a `tinygo build` of `config`. If goini fails,
`config.Parse`
gets a ~40-line built-in parser for the three keys used, and goini stays on
the host path.

## Steps

1. Create `bus` and `cli` by moving code out of `ftdi`; slim `ftdi/main.go`;
   split
   `config`. Run `go vet ./... && go test ./...` on the host.
2. Add the dependencies and write `nrf/storage.go`, `nrf/console.go` and
   `nrf/main.go`.
3. `cd src && tinygo build -target=feather-nrf52840 -o /tmp/.../knixx.uf2
   ./nrf`, and fix
   anything TinyGo rejects.

## Verification

- Host: `go vet ./... && go test ./...` (config `Parse`/`Default`, `cli.Exec`,
  and the
  moved bus tests). `go build ./ftdi`, then a piped `stat`/`exit` session
  against the
  attached adapter, as before.
- TinyGo build of `./nrf` succeeds; I'll report the reported flash/RAM size.
- On the board, yours to run:
  1. `tinygo flash -target=feather-nrf52840 ./nrf`, then open the serial
     console
     (`tinygo monitor`).
  2. First boot reports `formatted flash, wrote default knixx.ini`. A reset
     reports a
     mount with no format.
  3. `stat` shows `U_State.ind: no errors` and `mode Normal`.
  4. `sniff on`, then `switch A on` / `switch B off` switch the lights, ending
     in
     `U_FrameState.ind: ok` and `L_Data.con: positive`.

Out of scope: editing `knixx.ini` on the device from the CLI (e.g. `cat` or
`set`
commands). For now, changing the config means reformatting or adding such a
command later.
