# bolt usage lists the flags of each command

> **Status: not implemented.** Discarded by the user: "Discard the plan.
> We're not implementing this." (2026-10-21). Recovered from the planning
> session's transcript: the session reused one plan file for all its plans,
> so only the last one was left on disk.

## Context

`bolt` with no arguments, or with `help`, `-h`, `-help` or `--help`, prints a
fixed `usage` string (`src/host/bolt/main.go:21`). That string names the
commands `ota` and `term` but none of their flags (`-zip`, `-name`, `-addr`,
`-wait`), and sends the reader to `bolt <command> -h`. The goal is a usage
text that shows every command with its flags and defaults. It should come from
the flag sets themselves, so it cannot drift from them.

## Approach: build the usage from the FlagSets

All changes are in **`src/host/bolt/main.go`**:

1. **A command table.** Each command becomes an entry with:
   - its name and its one-line synopsis (the two lines now in `usage`);
   - a constructor that returns a `*flag.FlagSet` with its flags defined, plus
     a `run func() error` that reads the parsed flag values.

   `otaCmd`/`termCmd` become those constructors, for example `otaFlags()
   (*flag.FlagSet, func() error)`. The flag definitions stay exactly as they
   are now. `runOTA` and `runTerm` don't change.
2. **`printUsage(w io.Writer)`** writes:
   - the header `usage: bolt <command> [flags]`;
   - for each command, the line `bolt <name> [flags]   <synopsis>`;
   - each command's flags, from a fresh flag set via `fs.SetOutput(w);
     fs.PrintDefaults()`. This is the standard format: the name, the type, an
     indented description, then `(default ...)`.

   The line "Run bolt <command> -h for the flags of a command." is dropped,
   because the flags are now shown.
3. **Per command `-h`.** Each flag set's `fs.Usage` prints `usage: bolt <name>
   [flags]`, then the synopsis, then `PrintDefaults`. So `bolt ota -h` matches
   the matching section of the full usage, instead of Go's default `Usage of
   bolt ota:`.
4. **`main`** looks the command up in the table and replaces the `switch`.
   `help`, `-h`, `-help` and `--help` print the full usage to stderr and exit
   0, as they do now. No command, or an unknown one, still prints the full
   usage and exits 2. The package doc comment at the top stays as it is.

Expected output (abridged):

```
usage: bolt <command> [flags]

bolt ota [flags]    send a DFU package over BLE to the bootloader (run ota on the knixx console)
  -addr string
    	address of the bootloader, instead of matching -name: ...
  -name string
    	advertised name of the bootloader (default "AdaDFU")
  -wait duration
    	how long to wait for the bootloader (default 1m0s)
  -zip string
    	DFU package built by make dfu (default "build/knixx-dfu.zip")

bolt term [flags]   a terminal on the BLE console (run nus on on the knixx console)
  -addr string ...
```

## Test

Add `src/host/bolt/main_test.go` with `TestUsage`. It renders `printUsage`
into a `bytes.Buffer` and checks two things:
- every command name is present;
- for each command, every flag that its flag set defines (found with
  `fs.VisitAll`) appears as `-<flag>`.

## Docs

The README doesn't quote the usage text (`README.md:94,139,167,218`), so no
change is needed there. I'll check that again when implementing.

## Verification

- `cd src && go test ./host/...` and `make test`.
- `make bolt`, then run each of these and read the output:
  - `build/bolt` (exit 2), `build/bolt help` (exit 0), `build/bolt ota -h`,
    `build/bolt term -h`;
  - `build/bolt foo`, which shows the unknown command message and then the
    usage.
- `make nus` still connects. This checks that flag parsing still works after
  the refactor.
