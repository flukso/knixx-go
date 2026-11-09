# Identify the Feather nRF52840 Express revision (D vs E)

> **Status: not implemented.** Stopped by the user before any work
> (2026-10-17). Recovered from the planning session's transcript: the
> session reused one plan file for all its plans, so only the last one was
> left on disk.

## Context

The user wants to know which Feather revision they have, to tell whether the
back-feed
current is about 70 µA (rev D, AP2112) or about 20 µA (rev E, RT9080). The
silkscreen
carries no revision mark. This is an informational question, so no code
changes are
needed.

## How to tell (from the Adafruit rev D / rev E schematics and the AP2112 datasheet)

1. **Regulator marking** (the decisive check):
   - The regulator is the 5-pin SOT-23 near the USB/JST connectors, U2.
   - `G3P` means AP2112K-3.3TRG1 (from the AP2112 datasheet ordering table),
     so **rev D**.
   - Any other code means RT9080-3.3, so **rev E**. Richtek doesn't publish
     the RT9080
     marking codes; its datasheet says to ask sales.
2. **Supporting changes in the BOM**:
   - The LED resistors R2, R5 and R3 are 1 kΩ on rev D and 4.7 kΩ on rev E. If
     the
     resistors are marked, look for `102` vs `472`.
   - Q3 is a DMG2305 on rev D and an AO3401 on rev E.
   - D1 is an MBR120 on rev D and an MBR0540 on rev E.
3. **Functional difference**:
   - On rev E the on-board NeoPixel is powered from `NEO_PWR`, a GPIO, while
     on rev D it
     is powered from 3.3 V.
   - A rev E NeoPixel only lights when firmware drives `NEO_PWR` high. That
     makes it a
     software-visible tell, but it needs code.
4. **Purchase date**: rev E is dated 2025, so a board bought before then is
   rev D.

## Verification

None needed; this is a hardware inspection by the user.
