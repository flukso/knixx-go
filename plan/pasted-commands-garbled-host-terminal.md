# Pasted commands garbled: the cause is the host terminal, not the firmware

> **Status: not implemented.** Turned back with "Commit the changes.": the
> conclusion (use screen, not `tinygo monitor`) needed no code, and the
> pending fix of `console-lockup-on-paste.md` was committed as `968317a`
> instead (2026-10-17). Recovered from the planning session's transcript:
> the session reused one plan file for all its plans, so only the last one
> was left on disk.

## Context

Pasted lines arrived with runs of bytes replaced by the next byte. The user
found that
this happens only with `tinygo monitor`. With `screen`, five commands pasted
at once all
went through correctly. The firmware receive path, including the uncommitted
`patches/tinygo-0.42.0-nrf52840-usbd-single-dma.patch`, is therefore fine. The
board-side diagnosis (probe script, A/B builds) is dropped.

## Outcome

- **No firmware change.** The DMA patch stays: it is what stopped the paste
  lockup, and
  `screen` shows it passes pasted data intact.
- **For the console, use `screen /dev/ttyACM0`** (or picocom/minicom), not
  `tinygo monitor`, when pasting `config dump` output.
- I'll note this in memory as an environment fact, so I don't suspect the
  firmware again
  for garbled console input.
- The DMA patch is still uncommitted. It can go into a commit once you ask,
  together
  with a note in the commit message on what it fixes (the ISR `wfi` deadlock,
  and DMA
  serialisation).

## Verification

Already done by the user: 5 commands pasted at once via `screen` all
succeeded.
