# Front plate STEP and combined Thunder assembly

> **Status: completed.** Implemented in commit `988aa80` ("add the front
> plate STEP and its renders"), followed by `3627c99` and `d3a3d42`
> (2026-10-20). The geometry was refined in chat during the work: Ø12.2 mm
> holes, the slot centred on the VL53 with half-circle ends and 3 mm high,
> the plate 3 mm above the switch bodies, and a DXF output. Recovered from
> the planning session's transcript: the session reused one plan file for
> all its plans, so only the last one was left on disk.

## Context

`pcb/Knixx_Thunder_00.step` is the electronics. It is a 32 MB AP203 assembly
exported by ASCON, with about 200 products and colours, and it includes the
main board, the Feather, the sensor board (VL53L5CX, VCNL4040, SHT2x) and two
toggle switches. We want:
- a STEP of the steel front plate;
- a combined STEP of the electronics plus the plate.

Along the way, the sensor board has to move to its real height: its underside
sits 12 mm above the main board's top face.

The machine has no CAD tooling: no FreeCAD, cadquery, OCP or pythonocc. The
work is therefore scripted with OpenCascade through `cadquery-ocp`, installed
with pip into a venv in the scratchpad, so nothing is installed system-wide.
This needs network access for pip.

## Plate geometry

- **Size and material:** 80 × 80 × 1 mm steel, square corners.
- **Switch holes:** two Ø12 mm holes at ±20 mm in X from the plate centre.
- **Cutout:** a 20 × 4 mm rectangle, centred in X and 17 mm off the switch
  axis line in Y, toward the sensor board. Its long side is horizontal, along
  X.
- **XY placement:**
  - The plate centre is the midpoint of the two switch bushing axes, found
    from the switch solids.
  - The renders show the switch axes 40 mm apart, which matches the ±20 mm
    holes. The script checks this and stops if the spacing is more than 0.2 mm
    off.
- **Z placement:** the plate's underside is 6 mm above the top face of the
  toggle switch bodies, which is where the threaded bushing starts.
- **Standalone file:** the plate STEP is modelled in the same coordinate
  frame, so it drops into the assembly without a transform.

## Steps

1. **Set up the tools:** create a venv at `$SCRATCH/cad` and run `pip install
   cadquery-ocp` there.
2. **Inspect:** a script in the scratchpad loads the STEP with XCAF
   (`STEPCAFControl_Reader`), keeping names, colours and the assembly tree. It
   prints the top-level components with their bounding boxes, so that the
   following can be identified:
   - the main board (`Board` under `PCB_ASM…`) and the Z of its top face;
   - the sensor board subassembly: the component whose subtree holds `VL53L5CX
     WITH CONES`, `VCNL4040` and `SHT2x`;
   - the two toggle switches: their bushing axes, from the cylindrical faces,
     and the top Z of their bodies.

   I'll check the identification against the renders before going on.
3. **Lift the sensor board:**
   - Change the location of the sensor board's component in the tree by ΔZ =
     (main board top + 12) − (sensor board underside).
   - Leave everything else alone.
   - Report any parts that link the boards, such as header pins on the main
     board, that no longer reach.
4. **Make the plate:**
   - Build it with BRepPrimAPI box plus cuts: two cylinders and one box.
   - Write it to `pcb/Knixx_Thunder_00_frontplate.step` (AP214) with a light
     steel-grey colour and the product name `Front plate`.
5. **Unify:**
   - Add the plate as a new top-level component in the same XCAF document as
     the lifted electronics.
   - Write `pcb/Knixx_Thunder_00_assembly.step` (AP214), keeping names and
     colours.
   - The original `Knixx_Thunder_00.step` is not touched.
6. **Keep the generator:** the script is kept as `pcb/frontplate.py`, so that
   dimensions can be tweaked and the outputs regenerated. All dimensions are
   constants at the top. Running it needs only the `cadquery-ocp` venv.

## Verification

- **Re-read both outputs and print:**
  - the plate's bounding box, which must be 80 × 80 × 1;
  - its volume: 6400 − 2·π·36 − 80 = 6093.8 mm³;
  - the hole centres relative to the switch axes, within 0.05 mm.
- **Interference checks with BRepAlgoAPI_Common:**
  - the plate against every electronics solid, excluding the switch bushings
    and levers that pass through the holes. Any overlap volume is reported;
  - the lifted sensor board against the main-board parts.
- **Gaps to report:**
  - from the plate's underside to the top of the sensor parts;
  - whether the VL53L5CX cones fall within the cutout's footprint.
- **Renders:**
  - quick top and side PNGs of the assembly through OCP's offscreen viewer, if
    it works headless; otherwise matplotlib projections of the edges, compared
    against `pcb/*_top.png` and `*_side.png`;
  - you open the combined STEP in your viewer to confirm it.

Nothing is committed: `pcb/` is untracked and stays as you left it, apart from
the new files.
