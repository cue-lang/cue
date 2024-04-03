// Package cueversion provides access to the version of the
// cuelang.org/go module.
package cueversion

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
)

// fallbackVersion is used when there isn't a recorded main module version,
// for example when building via `go install ./cmd/cue`.
// It should reflect the last release in the current branch.
//
// TODO: remove once Go stamps local builds with a main module version
// derived from the local VCS information per https://go.dev/issue/50603.
const fallbackVersion = "v0.8.1"

// Version returns the version of the cuelang.org/go module as best as can
// reasonably be determined. The result is always a valid Go semver version.
func Version() string {
	return versionOnce()
}

var versionOnce = sync.OnceValue(func() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return fallbackVersion
	}
	switch bi.Main.Version {
	case "": // missing version
	case "(devel)": // local build
	case "v0.0.0-00010101000000-000000000000": // build via a directory replace directive
	default:
		return bi.Main.Version
	}
	return fallbackVersion
})

// UserAgent returns a string suitable for adding as the User-Agent
// header in an HTTP agent. The clientType argument specifies
// how CUE is being used: if this is empty it defaults to "cuelang.org/go".
//
// Example:
//
//	Cue/v0.8.0 (cuelang.org/go) Go/go1.22.0 (linux/amd64)
func UserAgent(clientType string) string {
	if clientType == "" {
		clientType = "cuelang.org/go"
	}
	// The Go version can contain spaces, but we don't want spaces inside
	// Component/Version pair, so replace them with underscores.
	// As the runtime version won't contain underscores itself, this
	// is reversible.
	goVersion := strings.ReplaceAll(runtime.Version(), " ", "_")
	return fmt.Sprintf("Cue/%s (%s) Go/%s (%s/%s)", Version(), clientType, goVersion, runtime.GOOS, runtime.GOARCH)
}
