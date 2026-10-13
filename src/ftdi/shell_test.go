package main

import "testing"

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
