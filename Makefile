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
HEX      := build/knixx.hex
DFU      := build/knixx-dfu.zip

# The sources of the firmware, so that the DFU package is only rebuilt when
# one of them changed.
FW_SRC := $(shell find src -name '*.go' -o -name '*.ini' -o -name '*.bin') src/go.mod src/go.sum

# The version embedded in the firmware: the short commit hash, marked -dirty
# when tracked files differ from it, and the build time in UTC. GIT_REFS
# makes the DFU package rebuild after a commit, which changes no source.
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)$(shell git diff --quiet HEAD 2>/dev/null || echo -dirty)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GIT_REFS   := $(wildcard .git/HEAD $(shell git rev-parse --git-path "$$(git symbolic-ref -q HEAD)" 2>/dev/null))
LDFLAGS    := -ldflags "-X knixx/version.Commit=$(GIT_COMMIT) -X knixx/version.Date=$(BUILD_DATE)"

# The goroutine stack size. TinyGo cannot size the stacks automatically for
# this firmware (its call graph reaches the runtime's recursive panic path),
# so they would fall back to the target's 2 KiB, too little for the console
# and fmt; a goroutine stack overflow followed. Stacks come from the heap.
STACK_SIZE := 8KB

export TINYGOROOT

.PHONY: nrf flash dfu ota test tinygo-root clean

nrf: $(STAMP)
	@mkdir -p build
	cd src && tinygo build -target=$(TARGET) -stack-size=$(STACK_SIZE) $(LDFLAGS) -size short -o ../$(FIRMWARE) ./nrf

flash: $(STAMP)
	cd src && tinygo flash -target=$(TARGET) -stack-size=$(STACK_SIZE) $(LDFLAGS) ./nrf

# dfu builds the package for a BLE update of the application with the
# Adafruit bootloader; 0x00B6 is the firmware id of its S140 6.1.1.
dfu: $(DFU)

$(HEX): $(STAMP) $(FW_SRC) $(GIT_REFS)
	@mkdir -p build
	cd src && tinygo build -target=$(TARGET) -stack-size=$(STACK_SIZE) $(LDFLAGS) -o ../$(HEX) ./nrf

$(DFU): $(HEX)
	adafruit-nrfutil dfu genpkg --dev-type 0x0052 --sd-req 0x00B6 --application $(HEX) $(DFU)

# ota waits for a board in BLE DFU mode (run ota on its console) and sends
# the DFU package over BlueZ. ADDR= selects a board by address.
ota: $(DFU)
	cd src && go run ./ota -zip ../$(DFU) $(if $(ADDR),-addr $(ADDR))

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
