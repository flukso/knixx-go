# Sniff without heap allocations

> **Status: completed.** Implemented in commit `53fe7d9` ("render the
> sniffer trace without heap allocations"), followed by `f83e488`
> (2026-10-21). Recovered from the planning session's transcript: the
> session reused one plan file for all its plans, so only the last one was
> left on disk.

## Context
With `sniff on`, heap in use climbed from ~86 KB to 136 KB in an hour and GCs
started: every bus frame the sniffer prints allocates. On a microcontroller the
trace path should allocate nothing, so that sniffing cannot fragment the heap or
add GC pauses. Goal: zero heap allocations per sniffed frame and per sniffed
service indication, with the same trace text (two small exceptions below).

## Where it allocates today (one frame = 3 lines)
- `tp1.Telegram.Unpack` (`src/tp1/tp1.go:147`): `tg.Data = make([]byte, length)`.
- `decoder.describe` (`src/bus/decode.go:114`): `strings.Builder` growth, six
  `fmt.Fprintf` calls (TinyGo's fmt allocates its printer state per call),
  `cemi.IndividualAddr/GroupAddr.String` (`fmt.Sprintf`), the returned string.
- `decoder.value` (`decode.go:162`): `kind.id()` does `new(dpt.DPT_x)`, string
  concatenation, and `DPT_9xxx.String` (`fmt.Sprintf("%.2f ...")`).
- `receiver.handle` (`src/bus/bus.go:139`): `fmt.Fprintf(Out, "  %s\n", ...)`
  and `fmt.Fprintf(Out, "  %s (%#02x)\n", ncn.Describe(s, v), v)`.
- `ncn.Describe` (`src/ncn/service.go:106`): `+` concatenation;
  `decodeFlags` → `strings.Join`; `DecodeSystemStat` concatenations + Join.

`bus.Out` is the console writer (`crlf` → `outq.Queue.WriteCRLF`), which copies
into the static ring, so a reused line buffer is safe to pass to it.

## Approach: append into one static line buffer
Everything becomes `appendX(dst []byte, ...) []byte` writing into a fixed array
owned by the receiver (`rx` in `bus.Run` is created once per Run).

1. **`src/tp1/tp1.go`** — `Unpack` reuses the capacity of `tg.Data`:
   `tg.Data = append(tg.Data[:0], buffer[apci:apci+length]...)`; update the doc
   comment ("in a new slice" → "reusing the capacity of Data"). Callers that
   pass a fresh Telegram (tests) behave as before.
2. **`src/bus/format.go`** (new, small) — `appendUint8`/`appendDec` (via
   `strconv.AppendUint`), `appendHex2`, `appendHexBytes` (the `% x` form),
   `appendIndividualAddr` ("a.b.c"), `appendGroupAddr` ("a/b/c"), and
   `appendF16(dst, data)`: the DPT 9 value computed exactly from the raw octets
   as hundredths (`m << e`, fits int32), printed with two decimals and sign.
3. **`src/bus/decode.go`**
   - `decoder` gains `tg tp1.Telegram`, `payload [15]byte` (seeded as
     `tg.Data = payload[:0]` before each Unpack) and static DPT values
     (`v1001 dpt.DPT_1001`, `v1018`, `v9001`, `v9004`, `v9007`).
   - `describe(frame) string` → `appendFrame(dst, frame) []byte`; `value` →
     `appendValue(dst, tg) []byte`, truncating back to its start when it has
     nothing to say (keeps today's rules). `dptKind.id()` returns the ID and a
     pointer to the decoder's static value instead of `new(...)`; the dpt
     `Unpack` is still used for the length/range check. DPT 1 text keeps
     `String()` (static strings); DPT 9 uses `appendF16` + unit
     (" °C", " lux", " %").
   - Keep a test-only `describe(frame) string` wrapper in `decode_test.go` so
     the existing table tests stay as they are.
4. **`src/ncn/service.go`** — `AppendDescribe(dst, s, b) []byte`, with
   `appendFlags` and `appendSystemStat` appending ", "-separated parts.
   `Describe`, `DecodeState`, `DecodeFrameState`, `DecodeSystemStat` stay as
   thin `string(Append...(nil, ...))` wrappers for tests/host use.
5. **`src/bus/bus.go`** — `receiver` gains `line [256]byte`; `handle` builds
   `"  " + frame/service + "\n"` in it and calls `Out.Write(line)` once.
   256 bytes covers the longest line (15 data octets, 1-char switch names from
   `config.switchNames`) with room to spare.

Output changes (both intentional, tests updated):
- The status byte prints as `0x03` instead of fmt's `%#02x` `0x3`.
- Large DPT 9 values print their exact value (e.g. 670760.96, not float32's
  670760.94). Typical values (21.00 °C, 40.00 %) are unchanged.

Out of scope: other heap users seen without sniff (318 → 678 objects in 5 min
this morning, e.g. ToF/sensor/console). The `-print-allocs` run below lists
them; I'll report them for a follow-up.

## Verification
- `make test`: existing `bus`, `ncn`, `tp1` tests pass (expected strings only
  change for the two items above).
- New host test in `bus`: `testing.AllocsPerRun` on `receiver.handle` with
  `Out = io.Discard`, fed a group write, a read, an individual-address frame and
  U_FrameState/L_Data.con/U_SystemStat bytes → 0 allocs. New `tp1` test: Unpack
  into a Telegram with preseeded Data → 0 allocs and no aliasing with the input.
- TinyGo: `cd src && tinygo build -target=feather-nrf52840 -print-allocs='knixx/(bus|ncn|tp1)' -o /dev/null ./nrf`
  shows no allocation sites in the sniff path (handle, appendFrame, appendValue,
  AppendDescribe, Unpack).
- Hardware (stop the soak with `pkill -f soak.sh` first, then `make flash`):
  `sniff on`, `sys` now and after ~10 min of bus traffic; compare object count
  and HeapInuse growth with the same interval with `sniff off`: sniff should add
  nothing. Restart the soak afterwards.
- Commit only when asked.
