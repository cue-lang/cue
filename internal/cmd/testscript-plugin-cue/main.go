// Command testscript-plugin-cue is a testscript plugin (see
// github.com/rogpeppe/go-internal/testscript/plugin) that mirrors the registry-related
// testscript support used by the cmd/cue tests in cuelang.org/go.
//
// It does not provide a cue command: a test runs its own cue executable via
// testscript's "exec cue". The plugin's job is to provision the environment
// that cue needs, in particular:
//
//   - per-test OCI module registries derived from _registry* directories,
//     exposed via CUE_REGISTRY* environment variables;
//   - the CUE_CACHE_DIR, CUE_LANGUAGE_VERSION and CUE_LANGUAGE_VERSION_BUGFIX
//     environment variables;
//   - a "memregistry" command that starts an in-memory OCI registry on demand
//     and sets a CUE_REGISTRY* variable to its host.
//
// Use it from a testscript test by enabling the plugin command (see
// github.com/rogpeppe/go-internal/testscript/plugin) and starting a
// script with "plugin cue".
package main

import (
	"fmt"
	"os"

	"github.com/rogpeppe/go-internal/testscript/plugin"
)

func main() {
	if err := plugin.Serve(newPlugin()); err != nil {
		fmt.Fprintln(os.Stderr, "testscript-plugin-cue:", err)
		os.Exit(1)
	}
}
