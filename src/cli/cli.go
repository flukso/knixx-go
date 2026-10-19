// Package cli implements the console commands shared by the host and the
// microcontroller builds.
package cli

import (
	"errors"
	"fmt"
	"io"
	"knixx/bus"
	"knixx/config"
	"knixx/thunder"
	"strings"
)

const (
	switchUsage = "usage: switch <A|B> <on|off>"
	sniffUsage  = "usage: sniff <on|off>"
	statUsage   = "usage: stat"
	configUsage = "usage: config [dump | init | get <key> | set <key> <value>[,<value>...]]"
)

var errUsage = errors.New(switchUsage)

// Cmd is a console command. Func writes its replies to w.
type Cmd struct {
	Name string
	Help string
	Func func(args []string, w io.Writer)
}

// Commands returns the console commands. Commands that talk to the
// transceiver are queued on cmds; configuration changes are applied to dev
// and saved to store.
func Commands(dev *thunder.Device, store config.Store, cmds chan<- bus.Command) []Cmd {
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
				Switch(dev, cmds, name, on, w)
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
		{
			Name: "config",
			Help: configUsage,
			Func: func(args []string, w io.Writer) {
				configCmd(dev, store, cmds, args, w)
			},
		},
	}
}

func configCmd(dev *thunder.Device, store config.Store, cmds chan<- bus.Command, args []string, w io.Writer) {
	switch {
	case len(args) == 0:
		list(dev, w)
	case len(args) == 1 && args[0] == "dump":
		// One set command per key, so that replaying them on another device
		// reproduces this configuration and its ini file exactly.
		for _, key := range config.Keys {
			v, _ := config.Get(dev, key)
			fmt.Fprintf(w, "config set %s %s\n", key, v)
		}
	case len(args) == 1 && args[0] == "init":
		def, err := config.Parse(config.Default)
		if err != nil {
			fmt.Fprintln(w, err)
			return
		}
		select {
		case cmds <- bus.Command{Kind: bus.CmdConfig, Dev: *def}:
		default:
			fmt.Fprintln(w, "tx queue full")
			return
		}
		*dev = *def
		// The embedded file is saved verbatim, so that the stored knixx.ini
		// is identical to the one in the firmware.
		if err := store.Save(config.Default); err != nil {
			fmt.Fprintf(w, "knixx.ini reset to the built-in default (not saved: %v)\n", err)
		} else {
			fmt.Fprintln(w, "knixx.ini reset to the built-in default (saved)")
		}
		list(dev, w)
	case len(args) == 2 && args[0] == "get":
		v, err := config.Get(dev, args[1])
		if err != nil {
			fmt.Fprintln(w, err)
			return
		}
		fmt.Fprintln(w, v)
	case len(args) >= 3 && args[0] == "set":
		// A group list may be typed with spaces after the commas.
		key := args[1]
		prev := *dev
		if err := config.Set(dev, key, strings.Join(args[2:], "")); err != nil {
			fmt.Fprintln(w, err)
			return
		}
		select {
		case cmds <- bus.Command{Kind: bus.CmdConfig, Dev: *dev}:
		default:
			*dev = prev
			fmt.Fprintln(w, "tx queue full")
			return
		}
		v, _ := config.Get(dev, key)
		if err := store.Save(config.Format(dev)); err != nil {
			fmt.Fprintf(w, "%s = %s (not saved: %v)\n", key, v, err)
			return
		}
		fmt.Fprintf(w, "%s = %s (saved)\n", key, v)
	default:
		fmt.Fprintln(w, configUsage)
	}
}

// list prints every configuration key with its value.
func list(dev *thunder.Device, w io.Writer) {
	for _, key := range config.Keys {
		v, _ := config.Get(dev, key)
		fmt.Fprintf(w, "%s = %s\n", key, v)
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

// Switch queues the command that sets the switch with the case-insensitive
// name to on. It is what the switch command does, and what the wall
// switches do; errors are written to w.
func Switch(dev *thunder.Device, cmds chan<- bus.Command, name string, on bool, w io.Writer) {
	i, ok := dev.SwitchIndex(name)
	if !ok {
		fmt.Fprintln(w, "unknown switch:", name)
		return
	}
	enqueue(w, cmds, bus.Command{Kind: bus.CmdSwitch, Switch: i, On: on})
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
