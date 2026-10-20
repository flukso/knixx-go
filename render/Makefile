# Renders the 3D model of the boards to the PNGs in pcb/: five views with a
# transparent background. frontplate makes the STEP of the front plate and of
# the boards with the plate. The STEP model itself is not in the repository;
# point STEP= at it if it is elsewhere. The Python packages are installed
# once in a venv here.

STEP   ?= ../pcb/Knixx_Thunder_00.step
OUTDIR ?= ../pcb
VENV   := .venv
PYTHON := $(VENV)/bin/python

BASE  := $(basename $(notdir $(STEP)))
VIEWS := iso_front iso_back top bottom side
PNGS  := $(foreach v,$(VIEWS),$(OUTDIR)/$(BASE)_$(v).png)
PLATE := $(OUTDIR)/$(BASE)_frontplate.step $(OUTDIR)/$(BASE)_assembly.step

.PHONY: render frontplate clean

render: $(PNGS)

# One run writes all five views.
$(PNGS) &: $(STEP) render_step.py $(VENV)/.installed
	$(PYTHON) render_step.py $(STEP) $(OUTDIR)

frontplate: $(PLATE)

# One run writes the plate and the assembly.
$(PLATE) &: $(STEP) frontplate.py $(VENV)/.installed
	$(PYTHON) frontplate.py $(STEP) $(OUTDIR)

$(VENV)/.installed: requirements.txt
	python3 -m venv $(VENV)
	$(PYTHON) -m pip install -q -r requirements.txt
	touch $@

clean:
	rm -rf $(VENV)
