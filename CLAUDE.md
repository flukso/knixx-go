# TinyGo Project: knixx

## Technical Stack
- **Language/Compiler:** Go with TinyGo Compiler
- **Target Microcontroller:** nrf52840
- **Display/Sensor Drivers:** [TinyGo Drivers](https://github.com)

## Common Commands
- Build project: `tinygo build -target=<target-device> -o main.hex main.go`
- Flash to hardware: `tinygo flash -target=<target-device> main.go`
- Run tests (using host standard Go): `go test ./...`; `make test` also runs `make allocs`
- Clean dependencies: `go mod tidy`

## Hard Coding Rules & Constraints
- **Main Block:** `func main()` is always required, even for WebAssembly (Wasm) compile targets.
- **Cgo & Assembler:** Avoid using native Cgo or complex Go assembly unless explicitly configuring hardware registers.
- **Panic Recover:** `recover()` is NOT fully supported on most TinyGo targets. Validate inputs defensively; do not rely on panic catching.
- **Reflection & Maps:** Minimize heavy reflection (`reflect` package) and keep map usage lightweight due to heavy memory overhead on small microcontrollers.
- **Concurrency:** TinyGo uses a custom cooperative scheduler. Use `runtime.Gosched()` inside infinite `for` loops if they block execution, preventing goroutine starvation.
- **No Native Multicore:** Write code assuming single-core execution contexts.

## Heap Allocations
- **No heap allocations in the firmware's steady state.** The TinyGo code under `src/` (everything but `src/host`) must not allocate in code that runs while the board works: the bus send and receive paths, the sniffer, the sensor, ToF, switch and LED loops, the console output and key echo. Allocations are acceptable only at startup, for configuration changes, in console commands, and on error paths.
- Build output into fixed-size buffers owned by a long-lived struct (`append(buf[:0], ...)`, `PackTo(*[N]byte)`), not with `fmt`, `strings.Builder`, string concatenation, `[]byte(string)` conversions, `make` or library calls that return new slices (e.g. knx-go's `dpt.Pack`). Keep errors as package-level `var`s, not `errors.New`/`fmt.Errorf` at the call. Do not pass a variable to `fmt` on a rare path in a hot function: TinyGo heap-allocates an escaping variable on entry to the function, whichever path runs. Local variables over 256 bytes go to the heap; keep large buffers package-level or in a long-lived struct.
- **Check every change to the firmware with `make allocs`** (part of `make test`). It must pass. When it reports a new call, remove the allocation; run `make allocs-update` only for a call outside the steady state, and say so in the commit message.
- Give new steady-state code a host test with `testing.AllocsPerRun(...) == 0`, as in `bus/decode_test.go` and `bus/bus_test.go`: `make allocs` does not see calls through interfaces.
- To find an allocation, `cd src && tinygo build -target=feather-nrf52840 -print-allocs=. -o /tmp/x.elf ./nrf` lists the compiler's heap objects by source line.

## Code Conventions
- Prefer explicit memory layouts and fixed-size arrays over slice allocations inside high-frequency execution loops.
- Use explicit data types matching microcontroller architectures (e.g., `uint32`, `int16`) rather than standard `int`.
- Structure internal peripheral drivers inside a local `pkg/` or `internal/` folder structure.
