package cmd

import (
	"fmt"
	"os"

	"cuelabs.dev/go/oci/ociregistry"
	"cuelabs.dev/go/oci/ociregistry/ociclient"

	"cuelang.org/go/internal/cueexperiment"
	"cuelang.org/go/internal/mod/modmux"
	"cuelang.org/go/internal/mod/modresolve"
)

func getRegistry() (ociregistry.Interface, error) {
	// TODO document CUE_REGISTRY via a new "cue help environment" subcommand.
	env := os.Getenv("CUE_REGISTRY")
	if !cueexperiment.Flags.Modules {
		if env != "" {
			fmt.Fprintf(os.Stderr, "warning: ignoring CUE_REGISTRY because modules experiment is not enabled. Set CUE_EXPERIMENT=modules to enable it.\n")
		}
		return nil, nil
	}
	resolver, err := modresolve.ParseCUERegistry(env, "registry.cuelabs.dev")
	if err != nil {
		return nil, fmt.Errorf("bad value for $CUE_REGISTRY: %v", err)
	}
	return modmux.New(resolver, func(host string, insecure bool) (ociregistry.Interface, error) {
		return ociclient.New(host, &ociclient.Options{
			Insecure: insecure,
		})
	}), nil
}
