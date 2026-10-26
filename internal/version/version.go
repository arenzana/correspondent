// Package version reports what is running. Both values are set at build
// time with -ldflags "-X github.com/arenzana/correspondent/internal/version.Version=…".
package version

// Version is the semantic version, or a git-describe string such as
// 0.1.0-3-gabc1234 for builds between releases.
var Version = "dev"

// Commit is the full git commit the binary was built from.
var Commit = ""

// Short returns the abbreviated commit.
func Short() string {
	if len(Commit) > 7 {
		return Commit[:7]
	}
	return Commit
}

// String is the human-readable form, e.g. "0.1.0 (abc1234)".
func String() string {
	if Commit == "" {
		return Version
	}
	return Version + " (" + Short() + ")"
}
