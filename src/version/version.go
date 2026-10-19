// Package version holds the version of the build, which the Makefile sets at
// link time.
package version

// Set with -ldflags "-X knixx/version.Commit=... -X knixx/version.Date=...".
var (
	// Commit is the short commit hash, with -dirty when tracked files
	// differed from it.
	Commit = "unknown"
	// Date is the build time in UTC, as 2006-01-02T15:04:05Z.
	Date = "unknown"
)

// String is the version line of the banner and the version command.
func String() string { return "version " + Commit + " built " + Date }
