// Package version exposes build-time identity for the BJJ CLI.
//
// The variables below are intended to be set via -ldflags at build time:
//
//	-X github.com/s1onique/bjj/internal/version.Version=...
//	-X github.com/s1onique/bjj/internal/version.Commit=...
//	-X github.com/s1onique/bjj/internal/version.BuildTime=...
//
// When unset, the package reports "dev" / "unknown" sentinels so that
// development builds remain self-describing without panic.
package version

// Info is the typed build-identity record surfaced by the CLI.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
}

// Current returns the build-time identity of the running binary.
func Current() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		BuildTime: BuildTime,
	}
}

// Version is the semantic version of the build.
var Version = "dev"

// Commit is the source commit the binary was built from.
var Commit = "unknown"

// BuildTime is the build timestamp in RFC3339 form.
var BuildTime = "unknown"
