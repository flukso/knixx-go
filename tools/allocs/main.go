// Command allocs lists the calls by which the firmware's own code reaches
// the heap, from the disassembly of the firmware ELF:
//
//	objdump -d --no-show-raw-insn knixx.elf | go run ./tools/allocs
//
// Every heap allocation ends in runtime.alloc. A function allocates when it
// calls runtime.alloc or a function that does, directly or through others.
// For each function of the firmware (package main and the knixx/ packages)
// that allocates, allocs prints one line per call to a function that
// allocates: "caller -> callee". Code the compiler outlined into shared
// OUTLINED_FUNCTION_ fragments counts as part of the function that calls it.
// Indirect calls, through an interface or a function value, are not seen,
// nor are the paths of a panic, which halts the firmware.
//
// With -baseline FILE, allocs compares the lines with FILE and fails when
// one is new; with -update it rewrites FILE instead.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
)

var (
	header = regexp.MustCompile(`^[0-9a-f]+ <(.+)>:$`)
	call   = regexp.MustCompile(`^\s*[0-9a-f]+:\s+(?:bl|blx|b\.w|b)\s+0x[0-9a-f]+ <([^>+]+)>`)
	hash   = regexp.MustCompile(`\.llvm\.[0-9]+$`)
)

const alloc = "runtime.alloc"

func name(s string) string { return hash.ReplaceAllString(s, "") }

func own(f string) bool {
	return strings.HasPrefix(f, "main.") || strings.Contains(f, "knixx/")
}

func outlined(f string) bool { return strings.HasPrefix(f, "OUTLINED_FUNCTION_") }

// ignored reports whether calls to f are left out: a panic, and the
// dispatch of an interface method call, which reaches every method of that
// name.
func ignored(f string) bool {
	return strings.HasPrefix(f, "runtime._panic") || strings.HasSuffix(f, "Panic") ||
		strings.HasPrefix(f, "runtime.runtimePanic") || strings.HasSuffix(f, "$invoke")
}

func main() {
	baseline := flag.String("baseline", "", "file of the accepted lines")
	update := flag.Bool("update", false, "rewrite the baseline file")
	flag.Parse()

	// calls maps each function to the functions it calls.
	calls := map[string]map[string]bool{}
	var cur string
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if m := header.FindStringSubmatch(line); m != nil {
			cur = name(m[1])
			continue
		}
		if m := call.FindStringSubmatch(line); m != nil && cur != "" {
			callee := name(m[1])
			if callee == cur || ignored(callee) {
				continue
			}
			if calls[cur] == nil {
				calls[cur] = map[string]bool{}
			}
			calls[cur][callee] = true
		}
	}
	if err := sc.Err(); err != nil {
		fail(err)
	}
	if len(calls) == 0 || calls[alloc] == nil && !called(calls, alloc) {
		fail(fmt.Errorf("no calls to %s in the disassembly", alloc))
	}

	// allocates holds the functions that reach runtime.alloc.
	allocates := map[string]bool{alloc: true}
	for changed := true; changed; {
		changed = false
		for f, cs := range calls {
			if allocates[f] {
				continue
			}
			for c := range cs {
				if allocates[c] {
					allocates[f], changed = true, true
					break
				}
			}
		}
	}

	// direct returns the callees of f, with outlined fragments replaced by
	// what they call.
	var direct func(f string, seen map[string]bool, out map[string]bool)
	direct = func(f string, seen, out map[string]bool) {
		for c := range calls[f] {
			if !outlined(c) {
				out[c] = true
			} else if !seen[c] {
				seen[c] = true
				direct(c, seen, out)
			}
		}
	}
	var lines []string
	for f := range calls {
		if !own(f) || !allocates[f] {
			continue
		}
		out := map[string]bool{}
		direct(f, map[string]bool{}, out)
		for c := range out {
			if allocates[c] {
				lines = append(lines, f+" -> "+c)
			}
		}
	}
	slices.Sort(lines)

	switch {
	case *baseline == "":
		for _, l := range lines {
			fmt.Println(l)
		}
	case *update:
		if err := os.WriteFile(*baseline, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			fail(err)
		}
		fmt.Printf("allocs: %d calls written to %s\n", len(lines), *baseline)
	default:
		check(lines, *baseline)
	}
}

func called(calls map[string]map[string]bool, f string) bool {
	for _, cs := range calls {
		if cs[f] {
			return true
		}
	}
	return false
}

// check compares lines with the baseline file and exits 1 on a new line.
func check(lines []string, baseline string) {
	data, err := os.ReadFile(baseline)
	if err != nil {
		fail(err)
	}
	accepted := map[string]bool{}
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		accepted[l] = true
	}
	var added []string
	for _, l := range lines {
		if !accepted[l] {
			added = append(added, l)
		}
		delete(accepted, l)
	}
	if len(accepted) > 0 {
		fmt.Printf("allocs: %d accepted calls are gone; make allocs-update drops them\n", len(accepted))
	}
	if len(added) > 0 {
		fmt.Fprintln(os.Stderr, "allocs: new calls that reach the heap in the firmware:")
		for _, l := range added {
			fmt.Fprintln(os.Stderr, "  +", l)
		}
		fmt.Fprintln(os.Stderr, "Remove the allocation, or, outside the steady state (startup,")
		fmt.Fprintln(os.Stderr, "configuration, console commands), accept it with make allocs-update.")
		os.Exit(1)
	}
	fmt.Printf("allocs: %d calls, all accepted in %s\n", len(lines), baseline)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "allocs:", err)
	os.Exit(1)
}
