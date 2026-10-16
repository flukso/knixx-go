package cli

import (
	"bytes"
	"knixx/bus"
	"knixx/light"
	"strings"
	"testing"
)

func TestParseSwitch(t *testing.T) {
	tests := []struct {
		args []string
		name string
		on   bool
		ok   bool
	}{
		{[]string{"A", "on"}, "A", true, true},
		{[]string{"b", "OFF"}, "b", false, true},
		{[]string{"A"}, "", false, false},
		{[]string{"A", "on", "now"}, "", false, false},
		{[]string{"A", "dim"}, "", false, false},
		{nil, "", false, false},
	}
	for _, tt := range tests {
		name, on, err := parseSwitch(tt.args)
		if (err == nil) != tt.ok || name != tt.name || on != tt.on {
			t.Errorf("parseSwitch(%q) = %q, %v, %v", tt.args, name, on, err)
		}
	}
}

func TestParseOnOff(t *testing.T) {
	tests := []struct {
		arg string
		on  bool
		ok  bool
	}{
		{"on", true, true},
		{"OFF", false, true},
		{"", false, false},
		{"bogus", false, false},
	}
	for _, tt := range tests {
		on, ok := parseOnOff(tt.arg)
		if on != tt.on || ok != tt.ok {
			t.Errorf("parseOnOff(%q) = %v, %v", tt.arg, on, ok)
		}
	}
}

func TestExec(t *testing.T) {
	dev := &light.Device{
		Individual: 0x7801,
		Switches: [light.NumSwitches]light.Switch{
			{Name: "A", Group: 0x0002},
			{Name: "B", Group: 0x0003},
		},
	}
	queue := make(chan bus.Command, 2)
	cmds := Commands(dev, queue)
	defer bus.Sniff.Store(false)

	tests := []struct {
		line   string
		quit   bool
		output string
		queued *bus.Command
	}{
		{"", false, "", nil},
		{"exit", true, "", nil},
		{"help", false, "switch", nil},
		{"bogus", false, `unknown command "bogus"`, nil},
		{"switch b ON", false, "", &bus.Command{Kind: bus.CmdSwitch, Sw: &dev.Switches[1], On: true}},
		{"switch C on", false, "unknown switch: C", nil},
		{"switch A", false, switchUsage, nil},
		{"stat", false, "", &bus.Command{Kind: bus.CmdStat}},
		{"stat now", false, statUsage, nil},
		{"sniff on", false, "", nil},
		{"sniff", false, "sniff is on", nil},
		{"sniff maybe", false, sniffUsage, nil},
	}
	for _, tt := range tests {
		var w bytes.Buffer
		if quit := Exec(cmds, tt.line, &w); quit != tt.quit {
			t.Errorf("%q: quit = %v", tt.line, quit)
		}
		if !strings.Contains(w.String(), tt.output) || (tt.output == "" && w.Len() != 0) {
			t.Errorf("%q: output %q, want %q", tt.line, w.String(), tt.output)
		}
		select {
		case got := <-queue:
			if tt.queued == nil || got != *tt.queued {
				t.Errorf("%q: queued %+v, want %+v", tt.line, got, tt.queued)
			}
		default:
			if tt.queued != nil {
				t.Errorf("%q: nothing queued, want %+v", tt.line, *tt.queued)
			}
		}
	}

	var w bytes.Buffer
	Exec(cmds, "stat", &w)
	Exec(cmds, "stat", &w)
	Exec(cmds, "stat", &w)
	if !strings.Contains(w.String(), "tx queue full") {
		t.Errorf("full queue: output %q", w.String())
	}
}
