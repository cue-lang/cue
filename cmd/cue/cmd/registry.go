package cmd

import (
	"fmt"
	"os"
	"sync"

	"cuelang.org/go/internal/cueexperiment"
	"cuelang.org/go/internal/mod/modload"
	"cuelang.org/go/mod/modconfig"
)

var ignoringCUERegistryOnce sync.Once

// getRegistryResolver returns an implementation of [modregistry.Resolver]
// that resolves to registries as specified in the configuration.
//
// If external modules are disabled and there's no other issue,
// it returns (nil, nil).
func getRegistryResolver() (*modconfig.Resolver, error) {
	if !modulesExperimentEnabled() {
		return nil, nil
	}
	return modconfig.NewResolver(nil)
}

func getCachedRegistry() (modload.Registry, error) {
	if !modulesExperimentEnabled() {
		return nil, nil
	}
	return modconfig.NewRegistry(nil)
}

func modulesExperimentEnabled() bool {
	if cueexperiment.Flags.Modules {
		return true
	}
	if os.Getenv("CUE_REGISTRY") != "" {
		ignoringCUERegistryOnce.Do(func() {
			fmt.Fprintf(os.Stderr, "warning: ignoring CUE_REGISTRY because modules experiment is not enabled. Set CUE_EXPERIMENT=modules to enable it.\n")
		})
	}
	return false

}
