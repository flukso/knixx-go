# Console line length 128 → 255

> **Status: completed.** Implemented in commit `720c72b` ("hold console
> lines of up to 255 characters") (2026-10-20). This is the approved
> revision; an earlier version was turned back with: "Make it 255 so that it
> fits into a uint8." Recovered from the planning session's transcript: the
> session reused one plan file for all its plans, so only the last one was
> left on disk.

## Context

The line editor cuts console lines at `lineedit.MaxLine` = 128 bytes, which
showed up when pasting over BLE. We want roughly twice that. 255 is the most
that still fits the editor's `uint8` lengths, so no types change.

## Changes

1. **`src/lineedit/lineedit.go`:**
   - `MaxLine = 255`.
   - Its comment says it is the longest length a `uint8` holds, and that it
     fits a `config set` line for a switch bound to the most group addresses
     with room for pasted lines.
   - `n`, `draftN` and `histN` stay `uint8`, and so do the `uint8(copy(...))`
     conversions.
   - The guard `int(e.n) < len(e.line)` stops at 255, so `e.n` never wraps.
   - `redraw` grows with `MaxLine` by itself.
2. **RAM:** about +1.3 KB (10 × 127 bytes), out of 26 KB used.
3. **README** (Size limits): "A console line holds at most 255 characters".
4. **No other dependencies.** `cli_test` and the lineedit long-line test both
   use `MaxLine`.

## Verification

- `go test ./lineedit ./cli` and `make test`.
- `make nrf`: check RAM goes up by about 1.3 KB.
- Flash, then:
  - paste a 250-byte line through `nusterm`, and check the echo and the
    `unknown command` reply each hold all 250 bytes;
  - recall it with the up arrow.
