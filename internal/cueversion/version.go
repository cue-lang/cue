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

// LanguageVersion returns the CUE language version.
// This determines the latest version of CUE that
// is accepted by the module.
func LanguageVersion() string {
	return "v0.9.0"
}

// ModuleVersion returns the version of the cuelang.org/go module as best as can
// reasonably be determined. This is provided for informational
// and debugging purposes and should not be used to predicate
// version-specific behavior.
func ModuleVersion() string {
	return moduleVersionOnce()
}

const cueModule = "cuelang.org/go"

var moduleVersionOnce = sync.OnceValue(func() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		// This might happen if the binary was not built with module support
		// or with an alternative toolchain.
		return "(no-build-info)"
	}
	cueMod := findCUEModule(bi)
	if cueMod == nil {
		// Could happen if someone has forked CUE under a different
		// module name; it also happens when running the cue tests.
		return "(no-cue-module)"
	}
	return cueMod.Version
})

func findCUEModule(bi *debug.BuildInfo) *debug.Module {
	if bi.Main.Path == cueModule {
		return &bi.Main
	}
	for _, m := range bi.Deps {
		if m.Replace != nil && m.Replace.Path == cueModule {
			return m.Replace
		}
		if m.Path == cueModule {
			return m
		}
	}
	return nil
}

// UserAgent returns a string suitable for adding as the User-Agent
// header in an HTTP agent. The clientType argument specifies
// how CUE is being used: if this is empty it defaults to "cuelang.org/go".
//
// Example:
//
//	Cue/v0.8.0 (cuelang.org/go; vxXXX) Go/go1.22.0 (linux/amd64)
func UserAgent(clientType string) string {
	if clientType == "" {
		clientType = "cuelang.org/go"
	}
	// The Go version can contain spaces, but we don't want spaces inside
	// Component/Version pair, so replace them with underscores.
	// As the runtime version won't contain underscores itself, this
	// is reversible.
	goVersion := strings.ReplaceAll(runtime.Version(), " ", "_")

	return fmt.Sprintf("Cue/%s (%s; lang %s) Go/%s (%s/%s)", ModuleVersion(), clientType, LanguageVersion(), goVersion, runtime.GOOS, runtime.GOARCH)
}
