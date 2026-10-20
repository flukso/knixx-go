"""Build the Thunder front plate and the combined assembly STEP.

usage: frontplate.py <model.step> <outdir>

Reads the STEP model of the boards and writes:
  <outdir>/<model>_frontplate.step  the plate alone, in the assembly frame
  <outdir>/<model>_assembly.step    the boards, with the sensor board moved
                                    to its standoff height, plus the plate
"""

import math
import os
import sys

from OCP.BRepAlgoAPI import BRepAlgoAPI_Cut
from OCP.BRepBndLib import BRepBndLib
from OCP.BRepGProp import BRepGProp
from OCP.BRepPrimAPI import BRepPrimAPI_MakeBox, BRepPrimAPI_MakeCylinder
from OCP.Bnd import Bnd_Box
from OCP.GProp import GProp_GProps
from OCP.IFSelect import IFSelect_RetDone
from OCP.Interface import Interface_Static
from OCP.Quantity import Quantity_Color, Quantity_TOC_RGB
from OCP.STEPCAFControl import STEPCAFControl_Reader, STEPCAFControl_Writer
from OCP.STEPControl import STEPControl_AsIs
from OCP.TCollection import TCollection_ExtendedString
from OCP.TDF import TDF_Label
from OCP.TDataStd import TDataStd_Name
from OCP.TDocStd import TDocStd_Document
from OCP.TopAbs import TopAbs_SHELL
from OCP.TopExp import TopExp_Explorer
from OCP.TopLoc import TopLoc_Location
from OCP.XCAFDoc import XCAFDoc_ColorSurf, XCAFDoc_DocumentTool, XCAFDoc_Location
from OCP.collections import Sequence_TDF_Label
from OCP.gp import gp_Ax2, gp_Dir, gp_Pnt, gp_Trsf, gp_Vec

# Plate, in mm. X runs between the switches, +Y towards the sensor board.
PLATE_SIZE = 80.0
PLATE_THICKNESS = 1.0
HOLE_DIAMETER = 12.2  # clearance for the M12 switch bushings
HOLE_OFFSET = 20.0  # from the centre, left and right
CUTOUT_WIDTH = 20.0  # of the rectangle; the half-circle ends add CUTOUT_HEIGHT
CUTOUT_HEIGHT = 3.0
# The cutout is centred in X, and in Y on the VL53L5CX (U3 on the sensor
# board), towards which +Y points.
VL53 = "U3"
PLATE_GAP = 3.0  # from the top of the toggle switch bodies to the plate
PLATE_COLOR = (0.62, 0.64, 0.66)

# The sensor board's underside sits this far above the main board's top.
SENSOR_STANDOFF = 12.0

TOLERANCE = 0.2  # allowed mismatch between switch spacing and hole spacing
BUSHING_DIAMETER = 12.0  # nominal, to tell the bushing from the other shells


def bbox(shape):
    b = Bnd_Box()
    BRepBndLib.Add_s(shape, b, True)
    p, q = b.CornerMin(), b.CornerMax()
    return p.X(), p.Y(), p.Z(), q.X(), q.Y(), q.Z()


def name(label):
    n = TDataStd_Name()
    if label.FindAttribute(TDataStd_Name.GetID_s(), n):
        return n.Get().ToExtString()
    return ""


class Model:
    def __init__(self, path):
        self.doc = TDocStd_Document(TCollection_ExtendedString("XmlOcaf"))
        r = STEPCAFControl_Reader()
        r.SetNameMode(True)
        r.SetColorMode(True)
        if r.ReadFile(path) != IFSelect_RetDone or not r.Transfer(self.doc):
            sys.exit(f"cannot read {path}")
        self.st = XCAFDoc_DocumentTool.ShapeTool_s(self.doc.Main())
        roots = Sequence_TDF_Label()
        self.st.GetFreeShapes(roots)
        if roots.Length() != 1:
            sys.exit(f"expected one root shape, found {roots.Length()}")
        self.root = roots.Value(1)

    def referred(self, label):
        if self.st.IsReference_s(label):
            ref = TDF_Label()
            self.st.GetReferredShape_s(label, ref)
            return ref
        return label

    def components(self, label):
        kids = Sequence_TDF_Label()
        self.st.GetComponents_s(self.referred(label), kids, False)
        return [kids.Value(i) for i in range(1, kids.Length() + 1)]

    def find(self, label, want, path=()):
        """Yields the component paths under label whose last name is want."""
        for c in self.components(label):
            p = path + (c,)
            if name(c) == want:
                yield p
            yield from self.find(c, want, p)

    def leaves(self, path):
        """Yields the paths of the parts under path."""
        kids = self.components(path[-1])
        if not kids:
            yield path
        for c in kids:
            yield from self.leaves(path + (c,))

    def world(self, path):
        """Returns the shape at the end of path placed in the root frame."""
        loc = TopLoc_Location()
        for c in path:
            loc = loc * self.st.GetLocation_s(c)
        return self.st.GetShape_s(self.referred(path[-1])).Moved(loc)


def switch_geometry(model, path):
    """Returns the bushing axis (x, y) and the body top z of a toggle switch.
    The body is the widest shell; the threaded bushing is the shell with a
    square footprint of about 12 mm that starts at the body top."""
    shells = []
    e = TopExp_Explorer(model.world(path), TopAbs_SHELL)
    while e.More():
        shells.append(bbox(e.Current()))
        e.Next()
    body = max(shells, key=lambda b: (b[3] - b[0]) * (b[4] - b[1]))
    bushings = [b for b in shells
                if abs((b[3] - b[0]) - BUSHING_DIAMETER) < 0.5
                and abs((b[4] - b[1]) - BUSHING_DIAMETER) < 0.5]
    if len(bushings) != 1:
        sys.exit(f"switch {name(path[-1])}: found {len(bushings)} bushings")
    b = bushings[0]
    return ((b[0] + b[3]) / 2, (b[1] + b[4]) / 2), body[5], b


def make_plate(cx, cy, z, cutout_y):
    s = PLATE_SIZE
    t = PLATE_THICKNESS
    plate = BRepPrimAPI_MakeBox(gp_Pnt(cx - s / 2, cy - s / 2, z), s, s, t).Shape()
    for dx in (-HOLE_OFFSET, HOLE_OFFSET):
        hole = BRepPrimAPI_MakeCylinder(
            gp_Ax2(gp_Pnt(cx + dx, cy, z - 1), gp_Dir(0, 0, 1)),
            HOLE_DIAMETER / 2, t + 2).Shape()
        plate = BRepAlgoAPI_Cut(plate, hole).Shape()
    cutout = BRepPrimAPI_MakeBox(
        gp_Pnt(cx - CUTOUT_WIDTH / 2, cutout_y - CUTOUT_HEIGHT / 2, z - 1),
        CUTOUT_WIDTH, CUTOUT_HEIGHT, t + 2).Shape()
    plate = BRepAlgoAPI_Cut(plate, cutout).Shape()
    # Half-circles on both ends make the cutout a slot.
    for dx in (-CUTOUT_WIDTH / 2, CUTOUT_WIDTH / 2):
        end = BRepPrimAPI_MakeCylinder(
            gp_Ax2(gp_Pnt(cx + dx, cutout_y, z - 1), gp_Dir(0, 0, 1)),
            CUTOUT_HEIGHT / 2, t + 2).Shape()
        plate = BRepAlgoAPI_Cut(plate, end).Shape()
    return plate


def add_plate(doc, st, plate, parent=None):
    """Adds the plate to doc, as a component of parent if given."""
    proto = st.AddShape(plate, False)
    TDataStd_Name.Set_s(proto, TCollection_ExtendedString("Front plate"))
    ct = XCAFDoc_DocumentTool.ColorTool_s(doc.Main())
    ct.SetColor(proto, Quantity_Color(*PLATE_COLOR, Quantity_TOC_RGB), XCAFDoc_ColorSurf)
    if parent is not None:
        comp = st.AddComponent(parent, proto, TopLoc_Location())
        TDataStd_Name.Set_s(comp, TCollection_ExtendedString("Front plate"))
    st.UpdateAssemblies()


def write(doc, path):
    Interface_Static.SetCVal_s("write.step.schema", "AP214IS")
    Interface_Static.SetCVal_s("write.step.unit", "MM")
    w = STEPCAFControl_Writer()
    w.SetNameMode(True)
    w.SetColorMode(True)
    if not w.Transfer(doc, STEPControl_AsIs) or w.Write(path) != IFSelect_RetDone:
        sys.exit(f"cannot write {path}")


def main():
    if len(sys.argv) != 3:
        sys.exit("usage: frontplate.py <model.step> <outdir>")
    model, outdir = sys.argv[1:]
    base = os.path.join(outdir, os.path.splitext(os.path.basename(model))[0])
    plate_out = base + "_frontplate.step"
    assembly_out = base + "_assembly.step"

    m = Model(model)

    # The main board is the component holding the switches; the other
    # top-level component is the sensor board.
    top = m.components(m.root)
    switches = [p for want in ("SW1", "SW2") for p in m.find(m.root, want)]
    if len(top) != 2 or len(switches) != 2:
        sys.exit(f"expected 2 top-level components and 2 switches, found {len(top)} and {len(switches)}")
    main_board = switches[0][0]
    sensor = next(c for c in top if not c.IsEqual(main_board))

    # The board itself is the flat plate spanning the assembly at z <= 0.
    board = next(p for p in m.find(main_board, "1"))
    board_top = bbox(m.world(board))[5]
    # The sensor board itself is the part with the largest footprint.
    def area(p):
        x0, y0, _, x1, y1, _ = bbox(m.world(p))
        return (x1 - x0) * (y1 - y0)
    sensor_board = max(m.leaves((sensor,)), key=area)
    sensor_bottom = bbox(m.world(sensor_board))[2]
    lift = board_top + SENSOR_STANDOFF - sensor_bottom
    t = gp_Trsf()
    t.SetTranslation(gp_Vec(0, 0, lift))
    XCAFDoc_Location.Set_s(sensor, TopLoc_Location(t) * m.st.GetLocation_s(sensor))
    m.st.UpdateAssemblies()
    print(f"main board top z={board_top:.2f}; sensor board underside "
          f"z={sensor_bottom:.2f} -> {sensor_bottom + lift:.2f} (moved {lift:+.2f})")

    geo = sorted(switch_geometry(m, p) for p in switches)
    (a, body_a, _), (b, body_b, _) = geo
    spacing = math.dist(a, b)
    if abs(spacing - 2 * HOLE_OFFSET) > TOLERANCE or abs(a[1] - b[1]) > TOLERANCE:
        sys.exit(f"switch axes {a} and {b}: {spacing:.2f} mm apart, holes are {2 * HOLE_OFFSET} mm")
    cx, cy = (a[0] + b[0]) / 2, (a[1] + b[1]) / 2
    z = max(body_a, body_b) + PLATE_GAP
    print(f"switch axes at ({a[0]:.2f}, {a[1]:.2f}) and ({b[0]:.2f}, {b[1]:.2f}); "
          f"body tops z={body_a:.2f}, {body_b:.2f}; plate underside z={z:.2f}")

    vl53 = [p for p in m.find(sensor, VL53)]
    if len(vl53) != 1:
        sys.exit(f"expected one {VL53} on the sensor board, found {len(vl53)}")
    _, y0, _, _, y1, _ = bbox(m.world((sensor,) + vl53[0]))
    cutout_y = (y0 + y1) / 2
    print(f"VL53L5CX centre y={cutout_y:.2f}, {cutout_y - cy:.2f} mm from the switch axis line")

    plate = make_plate(cx, cy, z, cutout_y)

    pdoc = TDocStd_Document(TCollection_ExtendedString("XmlOcaf"))
    add_plate(pdoc, XCAFDoc_DocumentTool.ShapeTool_s(pdoc.Main()), plate)
    write(pdoc, plate_out)

    add_plate(m.doc, m.st, plate, parent=m.root)
    write(m.doc, assembly_out)

    props = GProp_GProps()
    BRepGProp.VolumeProperties_s(plate, props)
    print(f"plate bbox {tuple(round(v, 2) for v in bbox(plate))}, volume {props.Mass():.1f} mm3")
    print(f"wrote {plate_out}\nwrote {assembly_out}")


if __name__ == "__main__":
    main()
