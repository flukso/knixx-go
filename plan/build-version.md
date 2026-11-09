# Build version: commit hash and build time in the banner and a `version` command

> **Status: completed.** Implemented in commit `3a62fa5` ("embed the commit
> and build time in the firmware") (2026-10-19). Recovered from the planning
> session's transcript: the session reused one plan file for all its plans,
> so only the last one was left on disk.

## Context

A board's firmware can now be replaced over BLE. Nothing on the board tells
which build it runs, so after `make ota` or `make flash` there is no way to
check from the console. Embed the short commit hash and the build date and
time at link time, show them in the console banner, and add a `version`
command that prints them.

## Approach

### Embedding: linker `-X`

TinyGo's `build` and `flash` take Go-compatible `-ldflags`, so string
variables are set at link time. This needs no generated source file and no
committed change per build.

**New package `src/version/version.go`** (host-testable, no build tag):

```go
// Package version holds the build's version, set by the Makefile at link time.
package version

// Set with -ldflags "-X knixx/version.Commit=... -X knixx/version.Date=...".
var (
	Commit = "unknown" // short commit hash, with -dirty for uncommitted changes
	Date   = "unknown" // build time, UTC, as 2006-01-02T15:04:05Z
)

// String is the version line of the banner and the version command.
func String() string { return "version " + Commit + " built " + Date }
```

**`src/version/version_test.go`:** checks `String()` for the default values
and for set values.

### Makefile

```make
# The version embedded in the firmware: the short commit hash, marked -dirty
# when tracked files differ from it, and the build time in UTC.
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)$(shell git diff --quiet HEAD 2>/dev/null || echo -dirty)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS    := -ldflags "-X knixx/version.Commit=$(GIT_COMMIT) -X knixx/version.Date=$(BUILD_DATE)"
```

- Add `$(LDFLAGS)` to the three tinygo invocations: `nrf`, `flash` and
  `$(HEX)`.
- `git diff --quiet HEAD` only looks at tracked files, so the untracked
  `docs/`, `test/`, `tinygo/` and `uf2/` don't make every build dirty.
- **Stale hash in the DFU package.** `$(HEX)` rebuilds only when sources
  change, so a commit with no source change would keep the old hash, or a
  stale `-dirty`. Fix: add `.git/HEAD` and the current branch's ref file to
  the `$(HEX)` prerequisites: `$(shell git rev-parse --git-path $$(git
  symbolic-ref -q HEAD || echo HEAD))`, with `$(wildcard ...)` to tolerate a
  packed ref.
- The build date is fixed once per `make` run; that is enough.

### Firmware

**`src/nrf/main.go`:**
- The banner becomes:
  ```
  === knixx thunder ===
  version c570a52 built 2026-10-19T12:34:56Z
  ```
  The second line is `version.String()`, so the banner and the `version`
  command print the same line. That means the `String()` above returns
  `"version " + Commit + " built " + Date`.
- Register `versionCmd()` in the command list.

**`src/nrf/version.go`** (`//go:build tinygo`, like `ota.go`):
- `versionCmd()` returns `cli.Cmd{Name: "version", Help: "show the commit and
  build time of the firmware"}`.
- It prints `version.String()` and rejects arguments with `usage: version`,
  the same pattern as `otaCmd`.
- Works over USB and NUS alike, since it writes to the `w` it is given.

### README

- Console table: a `version` row.
- Building section: one line saying `make nrf`/`flash`/`dfu` embed the short
  commit (with `-dirty`) and the UTC build time.
- Code layout: a `version` row.

## Files

- new: `src/version/version.go`, `src/version/version_test.go`,
  `src/nrf/version.go`
- changed: `Makefile`, `src/nrf/main.go`, `README.md`

## Verification

1. `make test` passes, including `version`.
2. `make nrf` builds. Check the strings are in the binary: `strings
   build/knixx.uf2 | grep -E 'c570a52|built'`, or check the ELF via a hex
   build. This confirms TinyGo applied `-X` to a non-main package.
3. Make a commit, then run `make dfu`: it rebuilds even with no source
   changes, and the new hash is embedded.
4. On the board after `make flash`:
   - The first key on USB shows the banner with the version line.
   - `version` prints the same line.
   - After `nus on`, `version` over BLE prints it too.
