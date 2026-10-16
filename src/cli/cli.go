// Package cli implements the console commands shared by the host and the
// microcontroller builds.
package cli

import (
	"errors"
	"fmt"
	"io"
	"knixx/bus"
	"knixx/light"
	"strings"
)

const (
	switchUsage = "usage: switch <A|B> <on|off>"
	sniffUsage  = "usage: sniff <on|off>"
	statUsage   = "usage: stat"
)

var errUsage = errors.New(switchUsage)

// Cmd is a console command. Func writes its replies to w.
type Cmd struct {
	Name string
	Help string
	Func func(args []string, w io.Writer)
}

// Commands returns the console commands. Commands that talk to the
// transceiver are queued on cmds.
func Commands(dev *light.Device, cmds chan<- bus.Command) []Cmd {
	return []Cmd{
		{
			Name: "switch",
			Help: switchUsage,
			Func: func(args []string, w io.Writer) {
				name, on, err := parseSwitch(args)
				if err != nil {
					fmt.Fprintln(w, err)
					return
				}
				sw := dev.Switch(name)
				if sw == nil {
					fmt.Fprintln(w, "unknown switch:", name)
					return
				}
				enqueue(w, cmds, bus.Command{Kind: bus.CmdSwitch, Sw: sw, On: on})
			},
		},
		{
			Name: "sniff",
			Help: sniffUsage,
			Func: func(args []string, w io.Writer) {
				switch len(args) {
				case 0:
					if bus.Sniff.Load() {
						fmt.Fprintln(w, "sniff is on")
					} else {
						fmt.Fprintln(w, "sniff is off")
					}
				case 1:
					on, ok := parseOnOff(args[0])
					if !ok {
						fmt.Fprintln(w, sniffUsage)
						return
					}
					bus.Sniff.Store(on)
				default:
					fmt.Fprintln(w, sniffUsage)
				}
			},
		},
		{
			Name: "stat",
			Help: "report the transceiver state and system status",
			Func: func(args []string, w io.Writer) {
				if len(args) != 0 {
					fmt.Fprintln(w, statUsage)
					return
				}
				enqueue(w, cmds, bus.Command{Kind: bus.CmdStat})
			},
		},
	}
}

// Exec runs the command on line. It reports whether the line asked to exit.
func Exec(cmds []Cmd, line string, w io.Writer) (quit bool) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false
	}
	switch fields[0] {
	case "exit":
		return true
	case "help":
		for _, c := range cmds {
			fmt.Fprintf(w, "  %-8s %s\n", c.Name, c.Help)
		}
		fmt.Fprintf(w, "  %-8s %s\n", "help", "list the commands")
		return false
	}
	for _, c := range cmds {
		if c.Name == fields[0] {
			c.Func(fields[1:], w)
			return false
		}
	}
	fmt.Fprintf(w, "unknown command %q, try help\n", fields[0])
	return false
}

// enqueue hands cmd to the transmit goroutine without blocking the console.
func enqueue(w io.Writer, cmds chan<- bus.Command, cmd bus.Command) {
	select {
	case cmds <- cmd:
	default:
		fmt.Fprintln(w, "tx queue full")
	}
}

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
