# Faster BLE console

> **Status: completed.** Implemented in commit `cc507be` ("speed up the BLE
> console") (2026-10-20). Recovered from the planning session's transcript:
> the session reused one plan file for all its plans, so only the last one
> was left on disk.

## Context

Typing on the BLE console (`nusterm`, or a phone app) feels sluggish. The aim
is lower keystroke-echo latency and better throughput, with no change to how
the console is used.

## Where the time goes

A typed key goes: host write → BLE connection event → SoftDevice → `nus.Ring`
→ console goroutine → line-editor echo → `nusOutQ` → sender goroutine →
notification → next connection event → host.

1. **Connection interval (the dominant cost).**
   - The central picks it. BlueZ uses the kernel defaults of 30–50 ms, and my
     earlier trace showed output batches about 43 ms apart.
   - Every echo needs at least one event for the write and one for the
     notification: roughly 45–90 ms per key.
   - The firmware never asks for anything faster. TinyGo's
     `sd_ble_gap_ppcp_set` advertises 7.5 ms–4 s, which is meaningless, and
     BlueZ ignores PPCP anyway.
   - The peripheral *can* request new parameters:
     `bluetooth.Device.RequestConnectionParams` →
     `sd_ble_gap_conn_param_update` (tinygo bluetooth
     `gap_nrf528xx-central.go`, built for s140v6). The Linux kernel accepts
     any spec-valid request.
   - macOS accepts it only within Apple's rules: interval ≥ 15 ms, and either
     max = 15 ms or max ≥ min + 15 ms.
   - **15 ms for both min and max** is valid everywhere and gives about 3×
     lower latency.
2. **`nusterm` serialises keys.**
   - Each read from stdin, i.e. one key, becomes a write with response that
     waits about 1–2 intervals.
   - Keys typed faster than about 10 per second therefore queue up behind each
     other, and the lag grows while typing.
   - Fix: coalesce everything typed while a write is in flight into the next
     write, still in MTU−3 chunks and still with response, so pastes keep
     their backpressure.
3. **Firmware polling adds about 7.5 ms on average:**
   - `consolePoll` = 10 ms, the console goroutine's sleep while no input is
     waiting (`src/nrf/console.go`);
   - `outputPoll` = 5 ms, the sender's idle sleep.

   Cutting both to 2 ms costs a negligible number of wake-ups and removes most
   of that delay for USB and NUS alike.
4. **Bulk output** (sniff, help) is limited to about 3 notifications of 20
   bytes per event, because TinyGo answers every MTU exchange with 23 bytes.
   At 15 ms that is about 4 KB/s instead of about 1.3 KB/s, which should be
   enough. A larger MTU (247) would mean patching tinygo bluetooth's
   SoftDevice config and its MTU reply. That is left out unless bulk output is
   still too slow after this change.

## Changes

1. **`src/nrf/nus.go`:**
   - Add `nusInterval = 15 * time.Millisecond` and `nusSupervision = 4 *
     time.Second`.
   - In `pollNUS`, when a connection is first seen (the `c && !wasConnected`
     branch, which runs on the console goroutine, not in the interrupt), call
     `nusCentral.RequestConnectionParams(bluetooth.ConnectionParams{MinInterval:
     bluetooth.NewDuration(nusInterval), MaxInterval:
     bluetooth.NewDuration(nusInterval), Timeout:
     bluetooth.NewDuration(nusSupervision)})`.
   - On error, print `nus: connection interval: <err>` on the console. The
     connection still works at the slower interval.
2. **`src/nrf/console.go`:** `consolePoll` 10 → 2 ms and `outputPoll` 5 → 2
   ms, with a comment on why.
3. **`src/nusterm/main.go`:**
   - The stdin goroutine appends to a mutex-guarded pending buffer and signals
     a channel with capacity 1.
   - The main loop takes the whole pending buffer and passes it through
     `chunks()` (escape handling unchanged), so keys typed during a write go
     out together in the next one.
   - `term.go` and its tests are unchanged. Add one test for a small `pending`
     type (append, take, the escape case) in `term_test.go`.
4. **README:**
   - The BLE console bullet mentions the 15 ms connection interval that the
     board requests.
   - The architecture note on the console mentions the 2 ms polls.

## Verification

- `make test`; `make nrf`, then flash.
- **Latency measurement before and after**, with a scratch Go program built on
  `nusterm`'s pieces:
  - Connect, then send one key at a time, 30 times, timing each from the write
    to its echo notification. Report the median and the 90th percentile.
  - Time the full `help` output.
  - I'll take the baseline on the current firmware first, then repeat with the
    new firmware.
  - Expected: a median echo time around 20–35 ms, down from about 60–90 ms.
- **Interactive check** with `make nus`: fast typing doesn't lag, a paste of
  about 300 bytes arrives whole, Ctrl-] and `nus off` still work.
- **USB console:** quick sanity check that it still behaves, given the 2 ms
  polls.
