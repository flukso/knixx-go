# Sniffer: infer the DPT of unbound groups from main/middle group

> **Status: completed.** Implemented in commit `ed09d0c` ("decode the
> telegrams on the bus while sniffing") (2026-10-20). This is the approved
> revision; an earlier version was turned back with: "Can't we cache the
> lineDPT lookup after initial population and drop the cache upon a config
> change? That would be a lot more efficient. Config changes a rare while
> packet decoding occurs quite frequent while sniffing a busy bus."
> Recovered from the planning session's transcript: the session reused one
> plan file for all its plans, so only the last one was left on disk.

## Context

A KNX telegram carries no DPT, so the sniffer (`src/bus/decode.go`) currently
decodes values only for the groups bound in the configuration. The user
reworked `src/config/default.ini` to give each sensor its own main group, with
middle group 0:

| binding | before | now | DPT |
|---|---|---|---|
| A, B | 0/0/1, 0/0/2 | unchanged | 1.001 |
| temperature | 0/1/1 | 2/0/1 | 9.001 |
| humidity | 0/1/2 | 3/0/1 | 9.007 |
| illuminance | 0/1/3 | 4/0/1 | 9.004 |
| occupancy | 0/2/1 | 5/0/1 | 1.018 |

New rule: a group that isn't bound, but shares main and middle group with a
binding, has the same DPT. Main and middle group are the high octet of the
16-bit group address (`g>>8`: 5 bits main, 3 bits middle).

## Design (`src/bus/decode.go`)

1. **A cached line table.** Config changes are rare and decoding on a busy bus
   is frequent, so the main/middle lookup is a table built once and dropped on
   a config change:
   - `type dptKind uint8` with `dptNone`, `dpt1001`, `dpt9001`, `dpt9007`,
     `dpt9004`, `dpt1018` and `dptConflict`. A small switch maps each kind to
     its ID string ("9.001") and a fresh `value` to unpack into.
   - `type lineTable struct { valid bool; kinds [256]dptKind }`, 256 bytes,
     indexed by `group>>8`. The `receiver` in `src/bus/bus.go` holds it next
     to `dev`.
   - **Populate lazily.** On the first decode while `valid` is false, walk the
     bindings: the switch groups, the sensor groups and the occupancy group,
     skipping unbound 0. For each, set `kinds[g>>8]` to its kind, or to
     `dptConflict` when the slot already holds a different kind, then set
     `valid`. Different DPTs on one line (possible with a custom `knixx.ini`)
     give no guess rather than a wrong one.
   - **Invalidate on a config change.** `Run`'s `CmdConfig` case sets
     `rx.lines.valid = false` after replacing `cur`. Nothing is built while
     sniff is off, and a change costs nothing until the next decoded frame.
   - Lookup is then `kinds[group>>8]`: O(1), no allocation.
   - **Exact lookup.** `binding(dev, group)`, the exact match that adds the
     binding name, stays a direct walk of at most 20 groups. Its kinds come
     from the same `kindOf` helper the table builder uses, so the two can't
     drift apart.
2. **`decodeValue`** output:
   - Exact binding, unchanged: `(A, DPT 1.001: On)` or `(temperature, DPT
     9.001: 21.00 °C)`.
   - Inferred: no binding name, since the group isn't ours: `(DPT 9.001: 22.50
     °C)`.
   - An inferred value that doesn't unpack, because the length doesn't fit the
     DPT, prints nothing. It shows the rule doesn't hold for that group, so
     the trace keeps only the raw bytes. An exact binding keeps printing name
     and DPT, as now.
   - Group reads of an inferred group show `(DPT 9.001)`.
   - Group 0/0/0 (broadcast) never gets a DPT.
3. The doc comments state the rule: same main/middle group, same DPT.

## Follow-ups of the default.ini change

- **`src/config/config_test.go:310`:** the expected default occupancy group
  0/2/1 becomes 5/0/1. This test currently fails. The other `0/1/x` uses in
  that file are explicit test values, not defaults, and stay.
- **`README.md`:**
  - the default `knixx.ini` listing (around line 327) and the defaults column
    of the configuration table (around line 354) get the new groups;
  - a sentence in the sniffer part of "Flow" explains the main/middle
    inference;
  - the example trace line stays valid.
- **Boards already in the field** keep their stored `knixx.ini` until `config
  init`; the README says so already in general, so there's nothing to change.

## Tests (`src/bus/decode_test.go`)

`decodeDevice()` moves to the new default groups, e.g. 2/0/1 and the others,
and gets new cases:

- **0/0/7 GroupValueWrite $01:** `(DPT 1.001: On)`, on the same line as A and
  B, with no name.
- **2/0/9 with 2 octets:** `(DPT 9.001: 21.00 °C)`.
- **4/0/2 and 5/0/3:** illuminance and occupancy inferred.
- **2/0/9 GroupValueRead:** `(DPT 9.001)`.
- **2/0/9 with a 1-octet value:** no bracket, since it doesn't decode.
- **2/1/1 (other middle group) and 6/0/1:** no bracket.
- **Conflict:** a device with temperature 2/0/1 and humidity 2/0/2, then 2/0/3
  gets no DPT.
- **0/0/0:** no DPT.
- **Cache invalidation, in a `Run`-level test with sniff on:**
  1. A frame to 2/0/9 decodes as DPT 9.001.
  2. A `CmdConfig` moves temperature to 6/0/1.
  3. A frame to 2/0/9 now has no DPT, and 6/0/9 decodes as DPT 9.001.

  This shows the table is rebuilt after the config change.

Existing exact-binding cases remain.

## Verification

1. Run `make test`; the config test is fixed, and the new decode cases pass.
2. Run `make nrf`; it builds with flash and RAM roughly unchanged.
3. **On the board.** With `sniff on`, send from another KNX device (or ETS) to
   a group on a bound line, e.g. 2/0/5. The trace shows the inferred DPT and
   value. A group on another line shows only raw bytes.
