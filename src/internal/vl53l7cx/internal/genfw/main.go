// Command genfw extracts the byte arrays of the ST ULD driver's
// vl53l7cx_buffers.h into binary files that package vl53l7cx embeds.
//
// Usage: genfw <vl53l7cx_buffers.h> <output dir>
package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// arrays maps the C array names to the files they are written to.
var arrays = map[string]string{
	"VL53L7CX_FIRMWARE":              "firmware.bin",
	"VL53L7CX_DEFAULT_CONFIGURATION": "default_configuration.bin",
	"VL53L7CX_DEFAULT_XTALK":         "default_xtalk.bin",
	"VL53L7CX_GET_NVM_CMD":           "get_nvm_cmd.bin",
}

// nbTarRanging is the value of VL53L7CX_FW_NBTAR_RANGING, which the
// default configuration uses symbolically, for VL53L7CX_NB_TARGET_PER_ZONE
// = 1.
const nbTarRanging = "0x02"

var (
	start = regexp.MustCompile(`^const uint8_t (\w+)\[\] = \{`)
	value = regexp.MustCompile(`0[xX][0-9a-fA-F]{1,2}\b`)
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: genfw <vl53l7cx_buffers.h> <output dir>")
		os.Exit(2)
	}
	if err := run(os.Args[1], os.Args[2]); err != nil {
		fmt.Fprintln(os.Stderr, "genfw:", err)
		os.Exit(1)
	}
}

func run(header, dir string) error {
	f, err := os.Open(header)
	if err != nil {
		return err
	}
	defer f.Close()

	found := map[string][]byte{}
	var name string
	var data []byte
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if name == "" {
			if m := start.FindStringSubmatch(line); m != nil {
				name, data = m[1], nil
			}
			continue
		}
		body, end := strings.CutSuffix(strings.TrimSpace(line), "};")
		body = strings.ReplaceAll(body, "VL53L7CX_FW_NBTAR_RANGING", nbTarRanging)
		for _, v := range value.FindAllString(body, -1) {
			b, err := strconv.ParseUint(v[2:], 16, 8)
			if err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
			data = append(data, byte(b))
		}
		if end {
			found[name] = data
			name = ""
		}
	}
	if err := sc.Err(); err != nil {
		return err
	}

	for array, file := range arrays {
		data, ok := found[array]
		if !ok {
			return fmt.Errorf("%s not found in %s", array, header)
		}
		if err := os.WriteFile(filepath.Join(dir, file), data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
