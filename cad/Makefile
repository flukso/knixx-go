# Renders the 3D model of the boards to the PNGs in pcb/: five views with a
# transparent background. frontplate makes the STEP and the DXF of the front
# plate, a DXF of the plate for each engraved curve, a DXF and a PNG of a
# sampler of nine of the curves, and the STEP of the boards with the plate.
# The STEP model itself is not in the repository; point STEP= at it if it is
# elsewhere. The Python packages are installed once in a venv here.

STEP   ?= ../pcb/Knixx_Thunder_00.step
OUTDIR ?= ../pcb
VENV   := .venv
PYTHON := $(VENV)/bin/python
# cadquery-ocp has wheels for Python 3.11 to 3.14 only; take the newest of
# those that is installed. Override with PY=.
PY     ?= $(firstword $(foreach v,14 13 12 11,$(shell command -v python3.$(v))) python3)

BASE  := $(basename $(notdir $(STEP)))
VIEWS := iso_front iso_back top bottom side
PNGS  := $(foreach v,$(VIEWS),$(OUTDIR)/$(BASE)_$(v).png)
# The engraved curves, as CURVES in frontplate.py names them.
CURVES := hypotrochoid lemniscate lissajous cardioid astroid nephroid deltoid \
          limacon cassini trefoil mystery teardrop fish bicorn
PLATE := $(OUTDIR)/$(BASE)_frontplate.step $(OUTDIR)/$(BASE)_frontplate.dxf \
         $(foreach c,$(CURVES),$(OUTDIR)/$(BASE)_frontplate_$(c).dxf) \
         $(OUTDIR)/$(BASE)_sampler.dxf $(OUTDIR)/$(BASE)_sampler.png \
         $(OUTDIR)/$(BASE)_assembly.step

.PHONY: render frontplate clean

render: $(PNGS)

# One run writes all five views.
$(PNGS) &: $(STEP) render_step.py $(VENV)/.installed
	$(PYTHON) render_step.py $(STEP) $(OUTDIR)

frontplate: $(PLATE)

# One run writes the plate, its DXFs and the assembly.
$(PLATE) &: $(STEP) frontplate.py $(VENV)/.installed
	$(PYTHON) frontplate.py $(STEP) $(OUTDIR)

$(VENV)/.installed: requirements.txt
	rm -rf $(VENV)
	$(PY) -m venv $(VENV)
	$(PYTHON) -m pip install -q -r requirements.txt
	touch $@

clean:
	rm -rf $(VENV)
