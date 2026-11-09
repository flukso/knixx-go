# Bind a switch to multiple group addresses

> **Status: completed.** Implemented in commit `b687f44` ("bind a switch to
> multiple group addresses") (2026-10-16). This is the approved revision; an
> earlier version was turned back with: "Yes, and make sure to commit the
> previous work first." Recovered from the planning session's transcript:
> the session reused one plan file for all its plans, so only the last one
> was left on disk.

## Context

Each switch (A, B) currently drives exactly one group address
(`light.Switch.Group`). We
want one switch to control several groups, for example to switch lights that
sit on
different group addresses. As you suggested, `A.group` / `B.group` become a
comma-separated list, in `knixx.ini` and in the `config` CLI command. `switch
A on`
then sends one GroupValueWrite per bound group.

This builds on the uncommitted `config get/set` work currently in the tree
(the
`bus.CmdConfig` copy, the switch index in `bus.Command`,
`config.Get/Set/Format`). Commit
that first, then do this as a follow-up commit.

A KNX note, so you can decide knowingly: in ETS a communication object *sends*
on its
first group address only, and further addresses are listen-only. The usual way
to drive
several actuators from one button is to give them all the same group address.
Sending one
telegram per group, as planned here, is a valid alternative. It just means N
bus
telegrams per press.

## Step 0: commit the config get/set work

Before any change for this feature:
1. Run `go vet` and `go test ./...` and do a `tinygo build`.
2. Stage the 11 modified files under `src/` (bus, cli, config, ftdi/main.go,
   light,
   nrf/main.go, nrf/storage.go).
3. Commit with `[config|cli|bus|nrf] get and set knixx.ini parameters from the
   console`.

Leave `test/`, `tinygo/` and `uf2/` alone. The multi-group feature then gets
its own
commit once it's verified.

## Format

```ini
[A]
group = 0/0/1, 0/0/5, 1/2/3
```

- Entries are separated by commas, and whitespace around them is trimmed.
- 1 to `light.MaxGroups` entries (proposed: **8**). An empty entry, a
  duplicate or too
  many entries is a parse error naming the key.
- `Format` writes `0/0/1, 0/0/5`. A single group still formats exactly like
  `default.ini`, so the existing `TestFormatMatchesDefault` keeps passing and
  old files
  stay valid.
- CLI: `config set A.group 0/0/1, 0/0/5` (spaces allowed, because `set` joins
  `args[2:]`) or `config set A.group 0/0/1,0/0/5`. `config get A.group` prints
  the list.
  `set` replaces the whole list; there are no add/remove subcommands.

## Design

### `light` (portable, fixed-size per CLAUDE.md)

```go
const MaxGroups = 8

type Switch struct {
    Name    string
    Groups  [MaxGroups]uint16
    NGroups uint8
}

func (s *Switch) GroupList() []uint16 { return s.Groups[:s.NGroups] }

// Telegram builds the GroupValueWrite for one group.
func (d *Device) Telegram(group uint16, on bool) tp1.Telegram
```

`Switch` and `Device` stay comparable, so `==` in the tests and the by-value
`bus.CmdConfig` copy keep working.

### `config`

- Add a shared helper `parseGroups(value string, sw *light.Switch) error` that
  splits on
  `,`, trims each entry, parses it with `cemi.NewGroupAddrString`, and checks
  the count
  and duplicates. It fills a local `Switch` first and assigns it only on
  success, so a bad
  `set` still leaves the device untouched.
- `Parse` and `Set` both use it.
- `Get`/`Format` join with `", "` via `cemi.GroupAddr(g).String()`.
- `field()` now returns a kind (individual address or group list) instead of a
  `*uint16`.

### `bus`: send groups one at a time, gated on `L_Data.con`

Sending N telegrams back to back is unsafe. The NCN5121 transmits one frame at
a time
(including up to 3 NACK + 3 BUSY repetitions), and a new `U_L_DataStart.req`
mid-frame
risks the protocol errors we saw earlier. So `Run` gets a small send state
machine:

- `pending`: the current switch command plus the index of the next group to
  send.
- `awaiting`: true from `TxData` until the receiver reports `L_Data.con`, or
  until
  `conTimeout` (proposed 500 ms, which covers the worst case of 7
  transmissions plus
  idle gaps). On a timeout it prints `no L_Data.con for 0/0/5`.
- While a command is pending or awaiting, the `cmds` case is disabled (a nil
  channel in
  the `select`). Queued commands, including `stat` and `config`, wait their
  turn, so the
  order is preserved. The RX ticker keeps running.
- `receiver.handle` sets a `gotCon`/`conPositive` result whenever it parses
  `L_Data.con`, whether or not sniff is on. With sniff off, a negative
  confirmation is
  still printed (`L_Data.con: negative for 0/0/5`), so a failed group is
  visible.
- Side benefit: single-group switches are also serialised now, so rapid
  `switch`
  commands can no longer overlap on the transceiver.

### `cli`

- `config set` joins `args[2:]` with `""` before calling `config.Set`, so
  `0/0/1, 0/0/5` typed with spaces works. Usage becomes
  `config [get <key> | set <key> <value>[,<value>…]]`.
- `switch` is unchanged; it still queues the switch index.

### Not changed

`ncn`, `tp1`, `nrf/*` and `ftdi/main.go` don't touch `Group` directly, so they
compile
as is. `default.ini` and `src/knixx.ini` remain valid single-group files.

## Files

| File | Change |
|---|---|
| `src/light/light.go`, `light_test.go` | `MaxGroups`, `Groups`/`NGroups`, `GroupList`, `Telegram(group, on)` |
| `src/config/config.go`, `config_test.go` | `parseGroups`, list Get/Set/Format; tests for lists, spacing, duplicates, too many, empty entry, round trip |
| `src/bus/bus.go`, `bus_test.go` | pending/awaiting state machine, L_Data.con gating and timeout |
| `src/cli/cli.go`, `cli_test.go` | join `set` args; usage text; a list set/get test |

## Verification

- `cd src && go vet ./bus ./cli ./config ./light ./ftdi && go test ./...`
- bus tests use the fake device:
  - a switch bound to 3 groups produces 3 telegrams in order. The fake answers
    each
    frame with `13 8b`; the test checks the second frame is only written after
    the
    first `8b`.
  - no confirmation: each group times out with `no L_Data.con` and the next
    one is
    still sent. Override `conTimeout` in the test to keep it short.
  - a `stat` queued behind a multi-group switch runs after the last group.
- `tinygo build -target=feather-nrf52840 ./nrf` succeeds; I'll compare its
  size with
  today's 127.9 KB.
- Hardware (yours): `config set A.group 0/0/1, 0/0/2`, `sniff on`, `switch A
  on`. Expect
  two `>` frames, each followed by `U_FrameState.ind: ok` and `L_Data.con:
  positive`, and
  both lights switch.
