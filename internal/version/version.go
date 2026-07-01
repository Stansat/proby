// Package version holds build-time identification, injected via -ldflags.
package version

import "runtime"

// These are overridden at build time with:
//
//	-ldflags "-X github.com/stansat/proby/internal/version.Version=... \
//	          -X github.com/stansat/proby/internal/version.Commit=... \
//	          -X github.com/stansat/proby/internal/version.Date=..."
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// GoVersion reports the Go runtime version the binary was built with.
func GoVersion() string { return runtime.Version() }
