# Move the BLE host tools under src/host

> **Status: completed.** Implemented in commit `118376f` ("move the BLE host
> tools to src/host") (2026-10-21). Recovered from the planning session's
> transcript: the session reused one plan file for all its plans, so only
> the last one was left on disk.

## Context
`src/` mixes the packages the nRF firmware builds with host-only tools, so it
is not obvious what runs where. The BLE host tools `ota` and `nusterm`, and
the helpers only they use, move under `src/host/`. `ftdi` stays in `src/`
(user's choice).

Analysis (read-only):
- Feasible with one module: `src/go.mod` (module `knixx`) stays as is; only
  import paths change. No package outside `ota`/`nusterm` imports
  `internal/blescan` or `internal/legacydfu`, and `nrf` imports neither
  (checked with `go list -tags tinygo ./nrf`).
- Go's internal rule: `src/host/internal/...` is importable only from
  `src/host/...`, so the firmware cannot pull in the BLE/DFU host code by
  mistake. `src/internal/` keeps only the sensor drivers.
- `go run ./host/ota` still builds a binary named `ota`; `go vet ./...` and
  `go test ./...` in `src/` still cover the moved packages.
- A separate host module was considered and is not needed: it would split
  go.mod/go.sum for no gain here.

## Changes
1. `git mv` (history kept):
   - `src/ota` → `src/host/ota`
   - `src/nusterm` → `src/host/nusterm`
   - `src/internal/blescan` → `src/host/internal/blescan`
   - `src/internal/legacydfu` → `src/host/internal/legacydfu`
2. Imports: `knixx/internal/blescan` → `knixx/host/internal/blescan` and
   `knixx/internal/legacydfu` → `knixx/host/internal/legacydfu` in
   `src/host/ota/main.go` and `src/host/nusterm/main.go`.
3. `Makefile`: `go run ./ota` → `go run ./host/ota` (`ota:` target),
   `go run ./nusterm` → `go run ./host/nusterm` (`nus:` target).
   Also exclude `src/host` from `FW_SRC`, so that editing a host tool no
   longer rebuilds the DFU package: `find src -path src/host -prune -o ...`
   keeping the same `-name` tests (with `-print`).
4. `README.md`:
   - BLE console section: `go run ./nusterm` in `src/` → `go run ./host/nusterm`.
   - Code layout: a sentence that `host/` holds the BLE host tools and their
     helpers, which the firmware does not build; the table rows become
     `host/ota`, `host/nusterm`, `host/internal/blescan`,
     `host/internal/legacydfu`.
5. Package doc comments: check `blescan`, `legacydfu`, `ota`, `nusterm` for
   path mentions (`git grep` for the old paths); names like "ota and
   nusterm" stay correct.

## Verification
- `git grep -nE 'internal/(blescan|legacydfu)|\./(ota|nusterm)\b'` finds only
  the new `host/` paths.
- `make test` passes (vet + tests, including blescan, legacydfu, nusterm).
- `make -n ota nus` shows the new `go run ./host/...` paths;
  `cd src && go build -o /dev/null ./host/...`.
- `make nrf` still builds the firmware; `make -n dfu` after touching a file
  in `src/host` shows no rebuild, after touching `src/nrf/main.go` it does.
- Optional on hardware: `make nus` connects, as tested earlier on this Mac.
- Commit only when the user asks.
