# Expose littlefs over USB mass storage while NUS is on

> **Status: not implemented.** Stopped by the user during the feasibility
> check, before a plan was written; the findings below were all that came of
> it (2026-10-19). Recovered from the planning session's transcript: the
> session reused one plan file for all its plans, so only the last one was
> left on disk.

## Context

The request:

> Tinygo 0.43.0 adds support for USB mass storage. Could we expose the
> littlefs file system over usb when the nus is on? So both nus and msc
> active in parallel. We can add a littlefs FUSE on the linux host side. So
> mounting the fs should not be a problem. Check the feasibility and come
> with a plan.

## Findings

What I checked suggests it's doable, for when you come back to it:

- **USB:** TinyGo 0.43.0 can show the serial port and the drive at the same
  time. The PC only sees the drive after a USB reconnect, which the firmware
  can do itself when NUS turns on or off. While NUS is on the console is on
  BLE, so the serial port isn't in use then.
- **Flash:** the flash driver doesn't erase before writing, so the drive needs
  a small wrapper that erases each 4 KiB block before writing it. The firmware
  also has to unmount littlefs and hold back `config set` saves while the PC
  has the drive.
- **PC side:** a FUSE mount of littlefs needs block size 4096, read and prog
  size 256, and 512 blocks on the 2 MiB flash. The board uses littlefs 2.8
  (disk format 2.1).
- **Costs:** about 8 KiB of extra buffer RAM, plus a TinyGo storage task that
  wakes every 100 µs for as long as the firmware runs once the drive has been
  shown.
- **Open questions:** the two I asked about. One is the 60 s idle timeout
  pulling the drive away during a FUSE mount. The other is whether `knixx.ini`
  is reloaded after it was edited on the PC.
