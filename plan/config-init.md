# `config init`: restore the built-in knixx.ini

> **Status: completed.** Implemented in commit `19808fd` ("config init
> restores the built-in knixx.ini") (2026-10-17). Recovered from the
> planning session's transcript: the session reused one plan file for all
> its plans, so only the last one was left on disk.

## Context

At the moment the embedded `config.Default` (`src/config/default.ini`,
`//go:embed`) is only
written by `loadConfig` (`src/nrf/storage.go`) when `/knixx.ini` is missing.
Getting a board
back to factory settings means wiping the flash. `config init` replaces the
stored file with
the one in the firmware and applies it straight away.

## Design: all in `cli.configCmd` (`src/cli/cli.go`)

No new storage code is needed. The existing `config.Store` interface is
enough:
- on nrf it is `flashStore.Save` → `writeConfig`, an `O_CREATE|O_TRUNC` write
  to
  `/knixx.ini`;
- on ftdi it is `config.FileStore`.

Both builds get the command for free.

New case `len(args) == 1 && args[0] == "init"`:

1. **Parse** `config.Default` with `config.Parse` into a fresh device `def`.
   Report an
   error if it fails; that can't happen because a test covers it.
2. **Apply:** enqueue `bus.Command{Kind: bus.CmdConfig, Dev: *def}` without
   blocking.
   - If the queue is full, print `tx queue full` and change nothing, as `set`
     does.
   - Otherwise set `*dev = *def`. The bus, the sensor groups and the switch
     bindings then
     take effect immediately, with no reboot.
3. **Save** the **embedded bytes verbatim** with `store.Save(config.Default)`,
   not
   `config.Format(def)`.
   - This makes the file on flash byte-identical to the firmware's, including
     any
     comments a future `default.ini` might have.
   - Today the two are the same anyway; `TestFormatMatchesDefault` checks
     this.
4. **Report** in the same style as `set`:
   - `knixx.ini reset to the built-in default (saved)`;
   - or `… (not saved: <err>)` when the flash is unavailable. In that case the
     defaults
     are still applied in RAM, matching `set`.

Then print the resulting values in the same `key = value` listing as `config`
with no
arguments. I'll factor that existing loop into a small `list(dev, w)` helper,
so you
can see what you now have.

Usage becomes:
`usage: config [dump | init | get <key> | set <key> <value>[,<value>...]]`

There is no confirmation prompt, consistent with `set`. Running `config dump`
first gives
a backup that can be pasted back.

## Tests (`src/cli/cli_test.go`), `TestConfigInit`

1. Start from `Default` and modify it with `config.Set`: another individual
   address,
   `A.group` as a list, `sensor.humidity` unbound.
2. `config init`:
   - `*dev` equals the parsed `Default`;
   - `store.saved` equals `config.Default` byte for byte;
   - one `CmdConfig` with the default device is queued;
   - the output contains `(saved)` and `device.individual = 7.8.1`.
3. With a full queue: the output is `tx queue full\n`, and dev and store are
   untouched.
4. With `store.err` set: the output contains `not saved:`, and dev is still
   reset.
5. `config init extra` prints the usage.

## Files

- `src/cli/cli.go`: the init case, the `list` helper, and the usage string.
- `src/cli/cli_test.go`: `TestConfigInit`.

## Verification

1. Run `make test`, then `make nrf`.
2. On hardware:
   1. Run `config set A.group 1/1/1` and `config set sensor.humidity -`.
   2. Run `config init`. It should report saved and list the defaults.
   3. Reboot. `config` should still show the defaults, which proves the file
      on flash was
      replaced.
   4. `switch A on` should send to 0/0/1 again, visible with `sniff on`.
