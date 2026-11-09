# Intermittent `switch B on/off` failures: diagnosis and next steps

> **Status: completed.** Implemented in commit `a421856` ("stat command and
> decoding of NCN5121 indications") (2026-10-14). This is the approved
> revision; an earlier version was turned back with: "Don't run a stat
> command in a loop (for now), but rather add a "stat" command to the cli
> with detailed decoding of the "state" and "systemstate" replies."
> Recovered from the planning session's transcript: the session reused one
> plan file for all its plans, so only the last one was left on disk.

## Context

A test session on the FTDI adapter + NCN5121 HAT (`pcb/knixx_HAT_00.PDF`,
8-bit UART at
38400 bps) showed that most `switch B on/off` commands reach the bus, but some
fail, even
three in a row. The question is why. I decoded the RX bytes in the trace with
the NCN5121
datasheet (UART services, Tables 12/13 and "Send Frame Service").

## What the NCN5121 should return for each telegram we send

For an 8-bit UART, the expected reply to `bc 78 01 00 02 81 00 80 39` is:
- 9 echoed octets (every byte it puts on the bus comes back);
- `U_FrameState.ind`, which is `0x13` when clean;
- `L_Data.con`, which is `0x8b` when ACKed and `0x0b` when not.

## What the trace shows

1. **Every echo is corrupted, and only ever 0→1.** For example, we sent
   `bc 78 01 00 02 81 00 80 39` and got back `be 78 21 20 22 a1 20 a0 39`.
   Within a burst,
   the same bit is stuck at 1 in every byte (`+0x20`, then `+0x40`, then
   `+0x60`). The
   status bytes are hit too: `0xcb`/`0xeb` are `0x8b` with extra bits, which
   is not a valid
   service code. `0xdb`/`0x9b` set FrameState's reserved bit, which the
   datasheet says is
   always 0. So the NCN5121→FTDI line flips bits.
2. **Failed commands are protocol errors, not bus problems.** Failures return
   no echo, only
   status bytes:
   - `fe 17 17 17 17 17 17 17`: `0x17` is `U_State.ind` with **pe** set (the
     host sent an
     invalid command sequence).
   - `ff 47`: `0x47` is `U_State.ind` with **re** set (corrupted bytes from
     the host: bad
     stop bit, control octet or checksum). The NCN rejects the frame and sends
     nothing.
   - `f8 17 17 17 03 17 …`: `0x03` is `U_Reset.ind`.

   This is what happens when the NCN loses framing in our byte stream. It then
   reads the
   telegram's data octets as commands: `0x02` (in the destination 0/0/2) is
   `U_State.req`,
   which explains the run of `0x17` replies, and `0x01` (in source 7.8.1) is
   `U_Reset.req`, which explains the `0x03`. So the FTDI→NCN5121 line also
   corrupts or
   loses bytes.

## Root cause

The UART link between the FTDI adapter and the NCN5121 has bit errors **in
both
directions**. The Go code and the KNX bus are not the cause: the frame we
build is correct
(`ncn.TxData` uses 0x80, then 0x81–0x87, then End 0x48, matching Table 12),
and the echoes
of the commands that worked show the NCN did transmit them. One-way 0→1 flips
usually
mean an electrical problem: a logic low that isn't low enough, or a ground
offset between
the USB host and the bus-powered HAT. The most likely culprits, in order:

- **No GND wire, or a poor one,** between the FTDI adapter and the HAT. The
  HAT's ground
  is the KNX bus minus (NCN VSS), not the PC's ground.
- **The FTDI adapter is running 5 V I/O.** The NCN5121 is 3.3 V (`3V3` from
  its own DC2
  regulator), so the FTDI's VCCIO jumper must be set to 3.3 V. Driving 5 V
  into SDI/RXD
  pushes current through the clamp diodes into a supply budgeted from the bus,
  which
  could also explain the `U_Reset.ind`.
- **Long or loose jumper wires** on RPi.TX / RPi.RX (header pins 8/10).

## Next steps

### A. Hardware checks (you)

- Confirm the FTDI VCCIO is 3.3 V and that a GND wire runs to the HAT header
  GND (pin 6).
- Shorten the TX/RX/GND wires and scope SDO/TXD to check the low level is
  close to 0 V.

### B. Software changes (me): make failures visible and the link testable

1. **`src/ncn/ncn.go`**
   - `TxData` builds the whole request (2 × len bytes) in a fixed `[2*64]byte`
     buffer and
     sends it in **one** `Write`. That is one USB transfer instead of 9, so no
     gaps open
     between control and data bytes.
   - Add service codes for `U_StateReq`/`U_SystemStatReq` (0x02/0x0D) and a
     small
     `Decode(b byte) string` that labels bytes from Table 13: `L_Data.con
     +/-`,
     `U_State.ind[sc re te pe tw]`, `U_FrameState.ind[re ce te]`,
     `U_Reset.ind`.
2. **`src/ftdi/tx.go`**: after each RX hex line, also print the decoded status
   bytes
   when sniff is on (for example `< … 13 8b  [FrameState ok] [L_Data.con +]`),
   so a failed
   telegram shows up as `L_Data.con -`, `re` or `pe` instead of silence.
3. **`stat` command, one-shot, with detailed decoding.**
   - `src/ncn/ncn.go`: add `U_SystemStatReq = 0x0D`. Add decoders that return
     human-readable text:
     - `DecodeState(b byte)` for `U_State.ind` (`sc re te pe tw 1 1 1`). It
       prints every
       flag with its meaning: slave collision, receive error (host sent
       corrupted bytes),
       transmit error (sent 0 but read 1), protocol error (bad command
       sequence), thermal
       warning.
     - `DecodeSystemStat(b byte)` for the second byte of `U_SystemStat.ind`
       (`0x4b`,
       then a status byte). The bits are V20V (bit 7), VDD2 (6), VBUS (5),
       VFILT (4),
       XTAL (3), TW (2) and Mode (bits 1:0: Power-Up, Sync, Stop, Normal).
       Each regulator
       bit is shown as ok or out of range. The bit order comes from the
       datasheet's
       Figure 38 and its SAVEB note ("bits 3 to 7 … bit 2"); I'll double-check
       it against
       the figure while implementing.
   - `src/ftdi/tx.go`: the `command` type gets a kind (switch or stat). For
     stat, the
     goroutine writes `0x02 0x0D` in one `Write`, then reads for up to ~100
     ms. It
     picks out the `U_State.ind` byte (`b&0x07 == 0x07`) and the `0x4b` +
     status pair, and
     prints the decoded result. If a reply is missing it prints "no
     U_State.ind / no
     U_SystemStat.ind reply". If sniff is on, any other bytes read in that
     window go to the
     normal `<` dump. A stat's output is always printed, whatever the sniff
     setting.
     Limitation: a bus telegram arriving in the same 100 ms window could
     contain a
     look-alike byte. That's acceptable for a manual diagnostic.
   - `src/ftdi/main.go`: register `stat` (no arguments) and queue it on the
     existing
     channel.
   - Example output:
     ```
     U_State.ind 0x07: no errors
     U_SystemStat.ind 0xfb: V20V ok, VDD2 ok, VBUS ok, VFILT ok, XTAL ok, no thermal warning, mode Normal
     ```

Out of scope for now: automatically resyncing or retrying after `re`/`pe`, by
sending
`U_Reset.req` and resending. That only hides a link that corrupts bytes.
Revisit it once
the hardware is fixed.

## Verification

- `cd src && go vet ./... ; go test ./ncn ./light ./config ./ftdi`. New `ncn`
  tests: a
  single `Write` with the expected byte sequence, and `Decode` on the codes
  seen in this
  trace (`0x17` → pe, `0x47` → re, `0x03` → reset, `0x8b`/`0x0b` → con ±,
  `0x13` →
  FrameState ok). Add `DecodeState`/`DecodeSystemStat` table tests, and a tx
  test where a
  fake device returns `07 4b fb` to a stat command, checking that the request
  bytes are
  `02 0d`.
- On hardware, yours to run: run `stat` a few times by hand and check the
  regulator, bus
  and mode report. Then `sniff on` and `switch B on/off`: every telegram
  should end in
  `13 8b` with an uncorrupted echo once the GND/VCCIO issue is fixed.
