# bolt genpkg: build the DFU package without adafruit-nrfutil

> **Status: completed.** Implemented in commit `e23204e` ("build the DFU
> package with bolt genpkg instead of adafruit-nrfutil") (2026-10-21).
> Recovered from the planning session's transcript: the session reused one
> plan file for all its plans, so only the last one was left on disk.

## Context
`make dfu` (and so `make ota`) still needs Python's `adafruit-nrfutil`, only
for `adafruit-nrfutil dfu genpkg --dev-type 0x0052 --sd-req 0x00B6
--application build/knixx.hex build/knixx-dfu.zip` (Makefile:66). `bolt ota`
already reads and sends the package itself (`src/host/internal/legacydfu`).
Building the package in Go as `bolt genpkg` removes the last Python dependency;
the zip stays, so phone apps (nRF Connect, Bluefruit Connect) can still use it.

## The format (adafruit-nrfutil 0.5.3, nordicsemi/dfu/*.py; checked against
the current build/knixx-dfu.zip)
- **knixx.bin** (`nRFHex.tobinfile`): the hex's data from its lowest address
  (at least 0x1000, past the MBR) to its highest, gaps filled with 0xFF,
  addresses >= 0x10000000 (UICR) dropped, the size rounded up to a multiple
  of 4 with 0xFF.
- **knixx.dat**, the legacy init packet, 14 bytes, little endian:
  `device_type u16 (0x0052) | device_rev u16 (0xFFFF) | app_version u32
  (0xFFFFFFFF) | sd_req count u16 | sd_req u16... (0x00B6) | crc16 u16`.
  Today's: `5200 ffff ffffffff 0100 b600 ea9a`.
- **crc16** of the bin (`calc_crc16`, init 0xFFFF): CRC-16/CCITT-FALSE, the
  Nordic `crc16_compute`; "123456789" gives 0x29B1.
- **manifest.json**: `{"manifest": {"application": {"bin_file": "knixx.bin",
  "dat_file": "knixx.dat", "init_packet_data": {"application_version":
  4294967295, "device_revision": 65535, "device_type": 82, "firmware_crc16":
  <crc>, "softdevice_req": [182]}}, "dfu_version": 0.5}}`, keys sorted,
  indent 4, as nrfutil writes it.
- TinyGo's hex has only record types 00, 01 and 04; the parser also accepts
  02 (extended segment address) and ignores 03/05 (start addresses), and
  checks each record's checksum.

## Design

**`src/host/internal/legacydfu`** (extend; it already owns the format)
- `ihex.go`: `ReadHex(r io.Reader) (base uint32, image []byte, err error)`:
  Intel HEX to a contiguous image as above (0xFF fill, UICR dropped, min
  address clamped to 0x1000, size padded to 4). Errors on a bad checksum,
  length, record type, overlap, or a missing EOF record.
- `genpkg.go`:
  - `type InitPacket struct { DevType, DevRev uint16; AppVer uint32; SDReq []uint16 }`
    with `Marshal(crc uint16) []byte`.
  - `CRC16(data []byte) uint16`.
  - `NewPackage(bin []byte, ip InitPacket) *Package`, which fills `Bin` and
    `Dat`, so it is the same `Package` that `Read` returns and `Update` sends.
  - `(*Package).WriteZip(w io.Writer, name string, ip InitPacket) error`:
    `<name>.bin`, `<name>.dat`, `manifest.json` (structs whose fields are in
    sorted key order, `json.MarshalIndent(…, "", "    ")`).
- Doc comment: Open reads, and NewPackage builds, the packages of
  `adafruit-nrfutil dfu genpkg`.

**`src/host/bolt/genpkg.go`** + `main.go`: `bolt genpkg [flags] <app.hex> <out.zip>`
with `-dev-type 0x0052`, `-dev-rev 0xFFFF`, `-app-ver 0xFFFFFFFF` and
`-sd-req 0x00B6` (comma-separated), defaults those of the Feather's Adafruit
bootloader with S140 6.1.1. Writes to a temp file next to `out.zip` and
renames it, so a failed run leaves no half zip for make. Prints the bin size
and CRC. Added to the usage text and the doc comment.

**`Makefile`**
- `$(DFU): $(HEX) | bolt` → `build/bolt genpkg $(HEX) $(DFU)`. `bolt` is
  order-only, so it gets rebuilt but the package only when the hex changed.
  The 0x00B6 comment moves to `bolt genpkg`'s defaults.

**`README.md`**: drop `adafruit-nrfutil` from the requirements; `make dfu`
builds the package with `bolt genpkg`; the `host/bolt` and
`host/internal/legacydfu` rows of the layout table (builds and reads the
packages); the OTA section's step 1.

## Verification
- `go test ./host/...`:
  - `CRC16("123456789") == 0x29B1`;
  - `ReadHex`: a small hand-written hex with an 04 record, a gap and an odd
    end (0xFF fill, padding to 4), a bad checksum, a missing EOF;
  - the init packet of the knixx parameters with crc 0x9AEA is the 14 bytes
    above;
  - `WriteZip` then `Read` round-trips `Bin` and `Dat`;
  - when `build/knixx.hex` exists: builds the package and checks it with
    `Read` (as the uf2 test does with `build/knixx.uf2`).
- One-off, while nrfutil is still installed: `adafruit-nrfutil dfu genpkg …
  /tmp/a.zip` and `build/bolt genpkg build/knixx.hex /tmp/b.zip` on the same
  hex; `knixx.bin` and `knixx.dat` are byte-identical, `manifest.json`
  identical (the zip timestamps differ).
- `make test` (allocs unchanged: host code only); `make dfu` after
  `touch build/knixx.hex` rebuilds the zip, a second `make dfu` does not.
- Hardware: `make ota` with the bolt-built package (stop the soak first,
  restart it after); the board comes back with the new `version`.
- Commit only when asked.
