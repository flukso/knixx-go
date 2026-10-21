# Builds the nrf firmware with the system TinyGo install, or, when there are
# local patches for its version in patches/, with a patched copy of it: the
# install is then copied once into tinygo/root-<version> and patched there,
# and the copy is rebuilt whenever a patch changes. TinyGo 0.43.0 needs no
# patches.

TINYGO_VERSION := 0.43.0
TINYGO_SYSTEM  := /usr/local/lib/tinygo
PATCHES        := $(sort $(wildcard patches/tinygo-$(TINYGO_VERSION)-*.patch))

ifneq ($(PATCHES),)
TINYGOROOT := $(CURDIR)/tinygo/root-$(TINYGO_VERSION)
STAMP      := $(TINYGOROOT)/.knixx-patched
export TINYGOROOT
else
STAMP :=
unexport TINYGOROOT
endif

TARGET   := feather-nrf52840
FIRMWARE := build/knixx.uf2
HEX      := build/knixx.hex
DFU      := build/knixx-dfu.zip

# The sources of the firmware, so that the DFU package is only rebuilt when
# one of them changed. The host tools in src/host are not part of it.
FW_SRC := $(shell find src -path src/host -prune -o \( -name '*.go' -o -name '*.ini' -o -name '*.bin' \) -print) src/go.mod src/go.sum

# The version embedded in the firmware: the short commit hash, marked -dirty
# when tracked files differ from it, and the build time in UTC. GIT_REFS
# makes the DFU package rebuild after a commit, which changes no source.
GIT_COMMIT := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)$(shell git diff --quiet HEAD 2>/dev/null || echo -dirty)
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
GIT_REFS   := $(wildcard .git/HEAD $(shell git rev-parse --git-path "$$(git symbolic-ref -q HEAD)" 2>/dev/null))
LDFLAGS    := -ldflags "-X knixx/version.Commit=$(GIT_COMMIT) -X knixx/version.Date=$(BUILD_DATE)"

# The goroutine stack size. TinyGo cannot size the stacks automatically for
# this firmware (its call graph reaches the runtime's recursive panic path),
# so they would fall back to a default stack, which was 2 KiB before TinyGo
# 0.43.0 and too little for the console and fmt; a goroutine stack overflow
# followed. 0.43.0 defaults to 8 KiB on the nRF52840; the size stays explicit.
# Stacks come from the heap.
STACK_SIZE := 8KB

.PHONY: nrf flash dfu bolt ota nus test tinygo-root tinygo-version clean

nrf: $(STAMP) | tinygo-version
	@mkdir -p build
	cd src && tinygo build -target=$(TARGET) -stack-size=$(STACK_SIZE) $(LDFLAGS) -size short -o ../$(FIRMWARE) ./nrf

flash: $(STAMP) | tinygo-version
	cd src && tinygo flash -target=$(TARGET) -stack-size=$(STACK_SIZE) $(LDFLAGS) ./nrf

# dfu builds the package for a BLE update of the application with the
# Adafruit bootloader; 0x00B6 is the firmware id of its S140 6.1.1.
dfu: $(DFU)

$(HEX): $(STAMP) $(FW_SRC) $(GIT_REFS) | tinygo-version
	@mkdir -p build
	cd src && tinygo build -target=$(TARGET) -stack-size=$(STACK_SIZE) $(LDFLAGS) -o ../$(HEX) ./nrf

$(DFU): $(HEX)
	adafruit-nrfutil dfu genpkg --dev-type 0x0052 --sd-req 0x00B6 --application $(HEX) $(DFU)

# bolt is the host tool for a board over BLE, with this computer's
# Bluetooth; ota and nus run it. Go's build cache makes the rebuild cheap.
bolt:
	@mkdir -p build
	cd src && go build -o ../build/bolt ./host/bolt

# ota waits for a board in BLE DFU mode (run ota on its console) and sends
# the DFU package with bolt ota. ADDR= selects a board by address.
ota: $(DFU) bolt
	build/bolt ota -zip $(DFU) $(if $(ADDR),-addr $(ADDR))

# nus connects a terminal to the BLE console of a board (run nus on on its
# console) with bolt term. ADDR= selects a board by address.
nus: bolt
	build/bolt term $(if $(ADDR),-addr $(ADDR))

test:
	cd src && go vet ./... && go test ./...

# tinygo-root makes the patched copy, if there are patches.
tinygo-root: $(STAMP) | tinygo-version

# tinygo-version checks that the system install is the expected version. It
# is an order-only prerequisite, so it does not force rebuilds.
tinygo-version:
	@case "$$(env -u TINYGOROOT tinygo version)" in \
	  "tinygo version $(TINYGO_VERSION) "*) ;; \
	  *) echo "expected tinygo $(TINYGO_VERSION), got: $$(env -u TINYGOROOT tinygo version)" >&2; exit 1 ;; \
	esac

ifneq ($(PATCHES),)
$(STAMP): $(PATCHES) | tinygo-version
	rm -rf $(TINYGOROOT)
	cp -a $(TINYGO_SYSTEM) $(TINYGOROOT)
	set -e; for p in $(PATCHES); do \
	  echo "applying $$p"; patch -p1 --forward -d $(TINYGOROOT) < $$p; \
	done
	tinygo clean
	touch $@
endif

clean:
	rm -rf build
