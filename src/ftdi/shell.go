package main

import (
	"errors"
	"strings"
)

const (
	switchUsage = "usage: switch <A|B> <on|off>"
	sniffUsage  = "usage: sniff <on|off>"
)

var errUsage = errors.New(switchUsage)

// parseSwitch splits the arguments of a switch command into the switch name
// and the requested state.
func parseSwitch(args []string) (name string, on bool, err error) {
	if len(args) != 2 {
		return "", false, errUsage
	}
	on, ok := parseOnOff(args[1])
	if !ok {
		return "", false, errUsage
	}
	return args[0], on, nil
}

func parseOnOff(arg string) (on bool, ok bool) {
	switch strings.ToLower(arg) {
	case "on":
		return true, true
	case "off":
		return false, true
	}
	return false, false
}
