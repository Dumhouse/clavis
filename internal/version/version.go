// Package version holds build metadata injected at release time.
package version

import "runtime"

// These values are overridden with -ldflags "-X ...".
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// Full returns a human-readable version string.
func Full() string {
	return Version + " (" + Commit + ", " + Date + ", " + runtime.Version() + ")"
}
