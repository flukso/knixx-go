# Corrupted pasted commands after the USBD DMA patch

> **Status: not implemented.** Turned back: the corruption came from `tinygo
> monitor`, not the firmware; "When switching to screen instead, a group of
> 5 commands pasted at once all succeeded." See `console-lockup-on-paste.md`
> for the fix that was committed (2026-10-17). Recovered from the planning
> session's transcript: the session reused one plan file for all its plans,
> so only the last one was left on disk.

## Context

With `patches/tinygo-0.42.0-nrf52840-usbd-single-dma.patch` (uncommitted) the
console no
longer hangs on a paste, but pasted lines arrive corrupted. Typed lines are
fine.

## What the trace says

Aligning each received line with the line that was pasted shows a single,
consistent
signature:

```
sent  config set sensor.temperature -
got   config eet eenso..temperature -     s→e, s→e, r→.
sent  config set A.group
got   config et  A.group                  s e t ␣ → e t ␣ ␣
sent  config set
got   confg sett                          i g ␣ s e t → g ␣ s e t t
sent  config
got   cofii                               n f → f i
```

- **The length never changes.** In a run of 1 to 6 positions, each byte is
  replaced by
  the byte that **follows** it. The run then ends with that following byte
  appearing
  twice.
- **No byte is invented.** Every byte that appears belongs to the line, and
  the rest of
  the line is intact.
- **Where it happens:** in the receive path.
  - The executed command shows the same damage, as `unknown command "confg"`
    proves, so
    it isn't the echo.
  - The bytes come out of the CDC rx ring like that, or are already like that
    when the
    USB DMA copies them in.

**Ruled out by reading the code:**
- **lineedit:** `Feed` only appends; it can't replace a byte with the next
  one.
- **The CDC rx ring** (`machine/usb/cdc/ring.go`): it has a single producer
  (`cdcCallbackRx` in the ISR) and a single consumer (`ReadByte` in the
  console
  goroutine). `head` is stored only after the copy, and `ReadByte` reads at
  `tail`. Nothing
  there can give "next byte" substitutions.
- **The rx handler:** it copies `udd_ep_out_cache_buffer[2][:AMOUNT]`
  synchronously in the
  ENDEPOUT handling, before the endpoint is re-armed with `SIZE.EPOUT = 0`. In
  software,
  the buffer can't be overwritten while it is being read.

That leaves the **USBD OUT EasyDMA transfer itself** returning the endpoint
buffer one
byte late for part of a packet. This looks like a hardware-level read
misalignment, not a
logic bug.

### Prime suspect: my own patch polls the USBD while its DMA runs

- `waitDMA()` busy-reads `EVENTS_ENDEPOUT/ENDEPIN` in a tight loop. It runs in
  thread
  mode, in `sendViaEPIn` for every echoed character, and it does so exactly
  while a
  pasted packet's OUT DMA is in flight.
- Before the patch nothing touched the USBD during a transfer, so the console
  hung
  instead.
- The anomaly-199 writes to the undocumented `0x40027C1C` are the second
  suspect. I wrote
  them from memory of nrfx, and a wrong value there could plausibly upset the
  DMA.
- Typed input never overlaps an OUT DMA with an echo, which is why typing
  stays clean.

**I can't settle this from the code.** The next step is to measure, with small
experiments on the board.

## Diagnosis plan (the board is on `/dev/ttyACM0`; pyserial 3.5 is available)

### 1. Host probe

A scratchpad script, `probe.py`, that doesn't change the config.

- **Probe line:** it sends a fixed, harmless line, for example
  `zz the quick brown fox jumps over 0123456789 abcdefghij`. The command is
  unknown, so
  nothing is executed or saved.
- **Check:** it compares the echoed text (what the board received) with what
  was sent.
- **Modes, 50 times each:**
  - a. one `write()` of the whole line (Linux cdc-acm sends a single packet
    below 64 B);
  - b. one byte per `write()` + `flush()`, which gives many back-to-back
    1-byte packets;
  - c. one byte every 5 ms, like fast typing;
  - d. a line longer than 64 bytes, which gives two packets.
- **Output:** the corruption rate per mode, with a few diffs.
- **What it tells us:**
  - corruption within mode a points to the DMA of a single packet;
  - corruption only in mode b points to timing between packets.
- You need to close your terminal on `ttyACM0` while it runs.

### 2. A/B builds

Each build is flashed with `make flash` and then probed. Only the patch file
changes
between builds.

| Build | Change | Expectation if the hypothesis holds |
|---|---|---|
| A | current patch | corrupt |
| B | drop the 0x40027C1C writes | still corrupt → not 199-related |
| C | no polling during DMA (below) | clean |

**Build C, a design with no polling while a transfer runs:**
- A DMA start never waits.
- `sendViaEPIn` called while a transfer runs only marks the IN packet as
  *pending* and
  returns.
- The USBD ISR, on ENDEPOUT/ENDEPIN (both interrupts enabled), starts the
  pending
  transfer: IN first, then OUT.
- An OUT EPDATA that arrives while busy is likewise deferred, but not fetched.
  The
  endpoint NAKs until it is started, which is safe.
- The CPU then never reads USBD registers while the DMA is moving data. This
  is the model
  nrfx uses: the DMA is started from the event handlers, with `m_dma_pending`.

### 3. Optional raw capture

If B and C don't separate the cases, use a temporary debug hunk, never
committed, in
`cdcCallbackRx`. It keeps the last 16 raw OUT packets (length + bytes) and
prints them on
a console command. That shows whether the bytes were already wrong as they
came out of
the DMA.

The host side can be confirmed independently with
`sudo modprobe usbmon && sudo cat /sys/kernel/debug/usb/usbmon/<bus>u`, which
shows that
the host sends the right bytes and the packet sizes.

## Fix

I'll decide once the data is in, and expect it to be build C. It would become
the
content of `patches/tinygo-0.42.0-nrf52840-usbd-single-dma.patch`, replacing
the polling
version. The patch stays uncommitted until you've verified it.

## Verification

- `probe.py` reports 0 corrupt lines in all four modes on the final build.
- Hardware recheck:
  1. Paste each `config dump` line and check it is saved correctly.
  2. Browse history.
  3. With a bus monitor, check that the minute sensor telegrams continue.
- `make test` is unchanged; `make nrf` builds.
