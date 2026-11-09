# bolt uf2 mounts the bootloader's drive through udisks (D-Bus, in Go)

> **Status: completed.** Implemented in commit `e4a11c9` ("mount the
> bootloader's drive through udisks") (2026-10-21). Recovered from the
> planning session's transcript: the session reused one plan file for all
> its plans, so only the last one was left on disk.

## Context

`make flash` timed out with "no UF2 drive named FTHR840BOOT after 20s" because
nothing on this machine mounts removable drives. The session is awesome with
no desktop environment. Thunar's volman automount is off, and udisks2 mounts
only when a client asks it to. `bolt uf2` finds the drive only through
`/proc/mounts`, so it waited for a mount that never came.

The fix: when the bootloader's FAT drive exists but isn't mounted, `bolt uf2`
asks udisks2 over the system D-Bus to mount it. This is the same request that
`udisksctl mount` and Thunar make, but done in Go. udisks 2.9.4 is installed
here, and it has `Manager.ResolveDevice`, which needs udisks 2.7.3 or newer.
`github.com/godbus/dbus/v5` v5.1.0 is already in the module graph through
tinygo bluetooth.

## Changes

### 1. `src/host/internal/uf2/mount_linux.go` (new, `//go:build linux`)

`MountVolume(names []string) (mount string, err error)` does the following:
- Connects to the system bus with `dbus.ConnectSystemBus()` and closes it
  afterwards.
- For each name, calls
  `org.freedesktop.UDisks2.Manager.ResolveDevice({"label": name}, {})` on
  `/org/freedesktop/UDisks2/Manager`. This returns the block objects (`ao`).
  Matching by label avoids the `\x20` escaping in `/dev/disk/by-label`.
- Skips any object whose `org.freedesktop.UDisks2.Block.IdType` is not `vfat`,
  so it never mounts some other drive that happens to have the same label.
- Calls `org.freedesktop.UDisks2.Filesystem.Mount({"auth.no_user_interaction":
  true})` and returns the mount path (`s`). The `no_user_interaction` option
  means a polkit refusal comes back as an error, instead of hanging on a
  password prompt that nobody sees.
- Treats the D-Bus error `org.freedesktop.UDisks2.Error.AlreadyMounted` as
  success. The next `FindVolume` call then picks the drive up from
  `/proc/mounts`.
- Returns `ErrNoDrive`, a new exported sentinel, when no FAT block has one of
  the names. That is the normal case while the bootloader is still
  enumerating.

### 2. `src/host/internal/uf2/mount_other.go` (new, `//go:build !linux`)

`MountVolume` returns `ErrNoDrive`. macOS mounts `/Volumes/<name>` by itself.

### 3. `src/host/bolt/uf2.go`

- A new helper `findVolume(names)`:
  - It first tries `uf2.FindVolume`.
  - If that finds nothing, it calls `uf2.MountVolume`. On success it prints
    `status("mounted %s", mount)` and returns the result only if `InfoFile` is
    there, by calling `FindVolume` again.
  - If the mount fails with an error other than `ErrNoDrive`, it prints the
    error once (e.g. `cannot mount FTHR840BOOT: … ; mount it by hand`) and
    keeps polling. That way a manual mount still works.
- Both lookups in `runUF2` use the helper:
  - the first check, "the board is in its bootloader already": a double-tapped
    board on this machine is otherwise unmounted too, and would wrongly fall
    through to `appPort`;
  - `waitVolume`, during its 500 ms polls.
- Update the comment on `runUF2` and on `FindVolume`/`MountVolume` to describe
  this behaviour.

### 4. `src/go.mod`

`github.com/godbus/dbus/v5 v5.1.0` changes from `// indirect` to a direct
requirement. The version stays the same and nothing else is upgraded. Check
this with `git diff go.mod go.sum`.

### 5. `README.md:165`

The sentence "On Linux the drive must be automounted (udisks)" becomes:

> On Linux bolt asks udisks to mount the drive when no automounter has, as the user at the machine (polkit)

The `dialout` remark stays. Also update the table row for `host/internal/uf2`
(`README.md:264`) to read "finds or mounts the drive".

## Tests

D-Bus itself can't be unit tested here. `uf2_test.go` keeps its tests of
`fatMounts` and `unescape`. The only new pure logic is the error mapping, and
that is too thin to be worth a fake bus.

## Verification

- `cd src && go vet ./host/... && go test ./host/...`, then `make test`.
- `GOOS=darwin go build ./host/bolt`, to check that the non-Linux stub
  compiles. cgo is off for this check, so if the bluetooth package blocks the
  build, use `go vet` with `GOOS=darwin` on `./host/internal/uf2` only.
- On the hardware, with no automounter running:
  1. `make flash`. Expect "resetting…", then "mounted
     /media/icarus75/FTHR840BOOT", the write, and "the board runs the new
     firmware".
  2. Double-tap reset, then `make flash` again. The unmounted bootloader drive
     is mounted and flashed without the serial reset.
  3. Mount the drive by hand in Thunar before `make flash`. The existing
     `/proc/mounts` path is used.
