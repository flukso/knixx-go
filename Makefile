# Builds the nrf firmware with the system TinyGo install, or, when there are
# local patches for its version in patches/, with a patched copy of it: the
# install is then copied once into tinygo/root-<version> and patched there,
# and the copy is rebuilt whenever a patch changes. TinyGo 0.43.0 needs no
# patches.

TINYGO_VERSION := 0.43.0
# The root of the system install, asked of tinygo only when it is copied.
TINYGO_SYSTEM   = $(shell env -u TINYGOROOT tinygo env TINYGOROOT)
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

.PHONY: nrf flash dfu bolt ota term nus test allocs allocs-update tinygo-root tinygo-version clean

nrf: $(STAMP) | tinygo-version
	@mkdir -p build
	cd src && tinygo build -target=$(TARGET) -stack-size=$(STACK_SIZE) $(LDFLAGS) -size short -o ../$(FIRMWARE) ./nrf

# flash builds the firmware and flashes it over USB with bolt uf2, which
# resets the board into its UF2 bootloader, copies the file to its drive and
# passes once the board runs this build. PORT= picks the serial port.
flash: nrf bolt
	build/bolt uf2 -expect "version $(GIT_COMMIT) built $(BUILD_DATE)" $(if $(PORT),-port $(PORT)) $(FIRMWARE)

# dfu builds the package for a BLE update of the application with the
# Adafruit bootloader, with bolt genpkg.
dfu: $(DFU)

$(HEX): $(STAMP) $(FW_SRC) $(GIT_REFS) | tinygo-version
	@mkdir -p build
	cd src && tinygo build -target=$(TARGET) -stack-size=$(STACK_SIZE) $(LDFLAGS) -o ../$(HEX) ./nrf

# bolt is order-only: it is rebuilt, but the package only when the hex
# changed.
$(DFU): $(HEX) | bolt
	build/bolt genpkg $(HEX) $(DFU)

# bolt is the host tool for a board over USB and, with this computer's
# Bluetooth, over BLE; flash, dfu, ota, term and nus run it. Go's build cache makes the
# rebuild cheap.
bolt:
	@mkdir -p build
	cd src && go build -o ../build/bolt ./host/bolt

# ota waits for a board in BLE DFU mode (run ota on its console) and sends
# the DFU package with bolt ota. ADDR= selects a board by address.
ota: $(DFU) bolt
	build/bolt ota -zip $(DFU) $(if $(ADDR),-addr $(ADDR))

# term connects a terminal to the console of a board with bolt term: on
# USB when the board answers there, else on BLE (run nus on on its console).
# PORT= selects the USB serial port, ADDR= a board on BLE by address.
term: bolt
	build/bolt term $(if $(PORT),-port $(PORT)) $(if $(ADDR),-addr $(ADDR))

# nus connects a terminal to the BLE console of a board (run nus on on its
# console) with bolt term. ADDR= selects a board by address.
nus: bolt
	build/bolt term -ble $(if $(ADDR),-addr $(ADDR))

# test vets and tests every package on the host. src/ftdi needs libftdi
# through cgo, so it is left out when pkg-config does not find libftdi1.
HAVE_LIBFTDI := $(shell pkg-config --exists libftdi1 2>/dev/null && echo yes)
TEST_PKGS    := ./... $(if $(HAVE_LIBFTDI),,| grep -vx knixx/ftdi)

test: allocs
	$(if $(HAVE_LIBFTDI),,@echo "libftdi1 not found: skipping src/ftdi")
	cd src && pkgs="$$(go list $(TEST_PKGS))" && go vet $$pkgs && go test $$pkgs

# allocs checks the calls by which the firmware's own code reaches the heap,
# found by tools/allocs in the disassembly of the firmware, against
# allocs.txt, so that a change cannot add one unnoticed: the steady state of
# the firmware should not allocate. allocs-update rewrites allocs.txt, for a
# call that is accepted: outside the steady state, or on an error path.
#
# tools/allocs reads the output of LLVM's objdump, which disassembles any
# target. It is llvm-objdump in the PATH, else the newest versioned one, as
# Debian installs it (llvm-objdump-14), else objdump, which is LLVM's on
# macOS; GNU objdump on Linux only knows the host. OBJDUMP= overrides it.
ALLOCS_ELF := build/allocs.elf
LLVM_OBJDUMPS := $(subst :, ,$(PATH))
LLVM_OBJDUMPS := $(wildcard $(addsuffix /llvm-objdump,$(LLVM_OBJDUMPS))) \
	$(lastword $(sort $(wildcard $(addsuffix /llvm-objdump-*,$(LLVM_OBJDUMPS)))))
OBJDUMP ?= $(firstword $(LLVM_OBJDUMPS) objdump)

allocs: $(ALLOCS_ELF)
	@$(OBJDUMP) -d --no-show-raw-insn $(ALLOCS_ELF) | go run tools/allocs/main.go -baseline allocs.txt

allocs-update: $(ALLOCS_ELF)
	@$(OBJDUMP) -d --no-show-raw-insn $(ALLOCS_ELF) | go run tools/allocs/main.go -baseline allocs.txt -update

$(ALLOCS_ELF): $(STAMP) $(FW_SRC) | tinygo-version
	@mkdir -p build
	@cd src && tinygo build -target=$(TARGET) -stack-size=$(STACK_SIZE) -o ../$@ ./nrf

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
