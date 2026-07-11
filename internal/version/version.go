// Package version holds build identification, set via -ldflags at release time:
//
//	go build -ldflags "-X local-fusion/internal/version.Version=2.0.0 -X local-fusion/internal/version.Commit=$(git rev-parse --short HEAD)"
package version

// Version is the semantic version of this build.
var Version = "2.0.0-dev"

// Commit is the short git commit hash of this build.
var Commit = "unknown"

// String returns the human-readable version line.
func String() string {
	return Version + " (" + Commit + ")"
}
