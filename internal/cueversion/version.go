// Package cueversion provides access to the version of the
// cuelang.org/go module.
package cueversion

import (
	"runtime/debug"
	"sync"
)

// fallbackVersion is used when there isn't a recorded main module
// version, for example when building via `go install ./cmd/cue`. It
// should reflect the last release in the current branch.
//
// TODO: remove once Go stamps local builds with a main module version
// derived from the local VCS information per
// https://go.dev/issue/50603.
const fallbackVersion = "v0.8.0-alpha.5"

// Version returns the version of the cuelang.org/go module as best
// as can reasonably be determined. The result is always a valid Go
// semver version.
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
