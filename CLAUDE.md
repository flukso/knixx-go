# TinyGo Project: knixx

## Technical Stack
- **Language/Compiler:** Go with TinyGo Compiler
- **Target Microcontroller:** nrf52840
- **Display/Sensor Drivers:** [TinyGo Drivers](https://github.com)

## Common Commands
- Build project: `tinygo build -target=<target-device> -o main.hex main.go`
- Flash to hardware: `tinygo flash -target=<target-device> main.go`
- Run tests (using host standard Go): `go test ./...`
- Clean dependencies: `go mod tidy`

## Hard Coding Rules & Constraints
- **Main Block:** `func main()` is always required, even for WebAssembly (Wasm) compile targets.
- **Cgo & Assembler:** Avoid using native Cgo or complex Go assembly unless explicitly configuring hardware registers.
- **Panic Recover:** `recover()` is NOT fully supported on most TinyGo targets. Validate inputs defensively; do not rely on panic catching.
- **Reflection & Maps:** Minimize heavy reflection (`reflect` package) and keep map usage lightweight due to heavy memory overhead on small microcontrollers.
- **Concurrency:** TinyGo uses a custom cooperative scheduler. Use `runtime.Gosched()` inside infinite `for` loops if they block execution, preventing goroutine starvation.
- **No Native Multicore:** Write code assuming single-core execution contexts.

## Code Conventions
- Prefer explicit memory layouts and fixed-size arrays over slice allocations inside high-frequency execution loops.
- Use explicit data types matching microcontroller architectures (e.g., `uint32`, `int16`) rather than standard `int`.
- Structure internal peripheral drivers inside a local `pkg/` or `internal/` folder structure.
