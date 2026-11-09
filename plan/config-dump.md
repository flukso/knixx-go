# `config dump`: print the configuration as replayable CLI commands

> **Status: completed.** Implemented in commit `8d52985` ("config dump
> prints the configuration as set commands") (2026-10-17). Recovered from
> the planning session's transcript: the session reused one plan file for
> all its plans, so only the last one was left on disk.

## Context

Cloning a knixx's setup to another board currently means retyping every
`config set`.
`config dump` prints one `config set <key> <value>` line per parameter.
Pasting those lines
one by one into another knixx console must leave that board with a
byte-identical
`knixx.ini`.

## Why the round trip is exact

- `config.Format(dev)` (`src/config/config.go`) is a pure function of the
  `thunder.Device`, and every `config set` saves `Format(dev)` to flash.
- So two boards with equal `Device` values write identical files, whatever the
  target's
  file looked like before (hand edits and comments are rewritten on the first
  set).
- `config.Keys` covers every field of `Device` that `Format` writes:
  `device.individual`,
  `A.group`, `B.group`, `sensor.temperature`, `sensor.humidity`. The switch
  names are
  fixed (A, B).
- `config.Get` already returns every value in the form `config.Set` accepts:
  - a switch group list as `0/0/1, 0/0/5`; `configCmd` joins `args[2:]` with
    `""`, so
    the spaces are harmless;
  - an unbound sensor key as `-`, which `Set` maps back to 0, so `Format`
    omits it again.
    This also clears a binding that the target had and the source doesn't.
- Every key is emitted, including those equal to the defaults, so the result
  does not
  depend on the target's prior state.

## Blocker: line length on the nrf console

`lineedit.MaxLine = 80` silently truncates longer lines. The worst case is a
switch bound
to `thunder.MaxGroups` = 8 groups of the form `31/7/255`:

- With the `, ` separator: `config set A.group ` (19 bytes) + 8·8 + 7·2 = 97
  bytes.
- Even with a comma-only separator it would be 90 bytes.

So **raise `MaxLine` to 128** in `src/lineedit/lineedit.go`.
- The cost is 2·48 + 8·48 + 48 ≈ 0.5 KB of RAM, for line, draft, history and
  redraw.
- `n` and `histN` stay `uint8`.
- A test (below) pins the worst case against `lineedit.MaxLine`, so a later
  increase of
  `MaxGroups` can't silently break the dump again.

## Changes

### `src/cli/cli.go`

- Add a `case len(args) == 1 && args[0] == "dump":` to `configCmd`. It loops
  over
  `config.Keys` and prints `config set %s %s\n` with `config.Get(dev, key)`.
  - This is read-only: nothing is queued and nothing is saved.
  - The command syntax belongs to `cli`, so the loop lives here and not in
    `config`.
- Extend `configUsage` to `usage: config [dump | get <key> | set <key>
  <value>[,<value>...]]`.

### `src/lineedit/lineedit.go`

- `MaxLine = 128`. The existing tests use `MaxLine` symbolically, so they need
  no changes.

## Tests (`src/cli/cli_test.go`)

`TestConfigDump`:

1. **Source:** parse `config.Default` and set it to something non-default:
   - another individual address;
   - A bound to 8 groups of the widest form (`31/7/255`, `31/7/254`, …);
   - `sensor.humidity` unbound.
2. **Dump:** run `config dump` on the source and check:
   - one line per `config.Keys` entry, each starting with `config set `;
   - every line ≤ `lineedit.MaxLine` (a test-only import of `knixx/lineedit`
     in cli).
3. **Replay:** start a target from a *different* file, for example `Default`
   with
   `sensor.humidity` bound elsewhere. Feed each dumped line through
   `lineedit.Editor.Feed` (with `\r`) and then `cli.Exec`, draining the bus
   queue between
   lines.
4. **Assert:**
   - the target `*dev` equals the source `*dev`;
   - the target store's `saved` equals `config.Format(source)` byte for byte.
5. **Read-only:** `config dump` on its own saves nothing and queues nothing.

Feeding the lines through the real editor also proves they survive truncation.

## Verification

- Run `make test`, then `make nrf` (check the RAM/flash size delta).
- On hardware:
  1. On board 1, run `config dump`.
  2. Paste the lines one at a time into board 2. Each line should answer
     `<key> = <value> (saved)`.
  3. Compare `knixx.ini` on both boards. They should be identical; `config`
     (no args)
     prints the same values on both.

**Note:** pasting all five lines in one go is not guaranteed. Each `set`
writes the flash
and enqueues a `CmdConfig` (`bus.QueueLen`), and a fast paste could hit `tx
queue full`.
Line by line, as specified, is safe.
