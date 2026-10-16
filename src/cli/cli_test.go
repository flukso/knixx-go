package cli

import (
	"bytes"
	"errors"
	"knixx/bus"
	"knixx/config"
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
			light.NewSwitch("A", 0x0002),
			light.NewSwitch("B", 0x0003),
		},
	}
	queue := make(chan bus.Command, 2)
	var store fakeStore
	cmds := Commands(dev, &store, queue)
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
		{"switch b ON", false, "", &bus.Command{Kind: bus.CmdSwitch, Switch: 1, On: true}},
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

type fakeStore struct {
	saved []byte
	err   error
}

func (s *fakeStore) Save(data []byte) error {
	if s.err != nil {
		return s.err
	}
	s.saved = data
	return nil
}

func TestConfig(t *testing.T) {
	dev, err := config.Parse(config.Default)
	if err != nil {
		t.Fatal(err)
	}
	queue := make(chan bus.Command, 1)
	var store fakeStore
	cmds := Commands(dev, &store, queue)

	exec := func(line string) string {
		var w bytes.Buffer
		Exec(cmds, line, &w)
		return w.String()
	}

	if got := exec("config"); !strings.Contains(got, "device.individual = 7.8.1") ||
		!strings.Contains(got, "A.group = ") || !strings.Contains(got, "B.group = ") {
		t.Errorf("config: %q", got)
	}
	if got := exec("config get device.individual"); got != "7.8.1\n" {
		t.Errorf("config get: %q", got)
	}

	for _, line := range []string{
		"config get C.group",
		"config set A.group nope",
		"config set A.group",
		"config bogus",
	} {
		before := *dev
		got := exec(line)
		if got == "" || *dev != before || store.saved != nil || len(queue) != 0 {
			t.Errorf("%q: output %q, dev %+v, saved %q, queued %d", line, got, dev, store.saved, len(queue))
		}
	}

	if got := exec("config set b.group 1/2/3"); got != "b.group = 1/2/3 (saved)\n" {
		t.Errorf("config set: %q", got)
	}
	if v, _ := config.Get(dev, "B.group"); v != "1/2/3" {
		t.Errorf("B.group = %s", v)
	}
	if string(store.saved) != string(config.Format(dev)) {
		t.Errorf("saved %q", store.saved)
	}
	if got := <-queue; got.Kind != bus.CmdConfig || got.Dev != *dev {
		t.Errorf("queued %+v", got)
	}

	if got := exec("config set A.group 0/0/1, 0/0/5,1/2/3"); got != "A.group = 0/0/1, 0/0/5, 1/2/3 (saved)\n" {
		t.Errorf("config set list: %q", got)
	}
	if got := exec("config get a.group"); got != "0/0/1, 0/0/5, 1/2/3\n" {
		t.Errorf("config get list: %q", got)
	}
	<-queue

	store.err = errors.New("flash unavailable")
	if got := exec("config set A.group 0/0/9"); !strings.Contains(got, "not saved: flash unavailable") {
		t.Errorf("failed save: %q", got)
	}
	<-queue

	queue <- bus.Command{Kind: bus.CmdStat}
	before := *dev
	if got := exec("config set A.group 0/0/7"); got != "tx queue full\n" || *dev != before {
		t.Errorf("full queue: output %q, dev %+v", got, dev)
	}
}
