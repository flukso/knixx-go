# Builds the nrf firmware with a TinyGo root that carries the local patches
# in patches/. The system install is copied once into tinygo/root-<version>
# and patched there; it is rebuilt whenever a patch changes.

TINYGO_VERSION := 0.42.0
TINYGO_SYSTEM  := /usr/local/lib/tinygo
TINYGOROOT     := $(CURDIR)/tinygo/root-$(TINYGO_VERSION)
PATCHES        := $(sort $(wildcard patches/tinygo-$(TINYGO_VERSION)-*.patch))
STAMP          := $(TINYGOROOT)/.knixx-patched

TARGET   := feather-nrf52840
FIRMWARE := build/knixx.uf2

# The goroutine stack size. TinyGo cannot size the stacks automatically for
# this firmware (its call graph reaches the runtime's recursive panic path),
# so they would fall back to the target's 2 KiB, too little for the console
# and fmt; a goroutine stack overflow followed. Stacks come from the heap.
STACK_SIZE := 8KB

export TINYGOROOT

.PHONY: nrf flash test tinygo-root clean

nrf: $(STAMP)
	@mkdir -p build
	cd src && tinygo build -target=$(TARGET) -stack-size=$(STACK_SIZE) -size short -o ../$(FIRMWARE) ./nrf

flash: $(STAMP)
	cd src && tinygo flash -target=$(TARGET) -stack-size=$(STACK_SIZE) ./nrf

test:
	cd src && go vet ./... && go test ./...

tinygo-root: $(STAMP)

$(STAMP): $(PATCHES)
	@case "$$(env -u TINYGOROOT tinygo version)" in \
	  "tinygo version $(TINYGO_VERSION) "*) ;; \
	  *) echo "expected tinygo $(TINYGO_VERSION), got: $$(env -u TINYGOROOT tinygo version)" >&2; exit 1 ;; \
	esac
	rm -rf $(TINYGOROOT)
	cp -a $(TINYGO_SYSTEM) $(TINYGOROOT)
	set -e; for p in $(PATCHES); do \
	  echo "applying $$p"; patch -p1 --forward -d $(TINYGOROOT) < $$p; \
	done
	tinygo clean
	touch $@

clean:
	rm -rf build
