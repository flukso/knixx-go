package version

import "testing"

func TestString(t *testing.T) {
	if got := String(); got != "version unknown built unknown" {
		t.Errorf("default: %q", got)
	}
	defer func(c, d string) { Commit, Date = c, d }(Commit, Date)
	Commit, Date = "c570a52-dirty", "2026-10-19T12:34:56Z"
	if got := String(); got != "version c570a52-dirty built 2026-10-19T12:34:56Z" {
		t.Errorf("set: %q", got)
	}
}
