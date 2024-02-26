package cmd

import (
	"fmt"
	"os"
	"sync"

	"cuelang.org/go/internal/cueconfig"
	"cuelang.org/go/internal/cueexperiment"
	"cuelang.org/go/internal/mod/modload"
	"cuelang.org/go/internal/mod/modregistry"
	"cuelang.org/go/mod/modcache"
	"cuelang.org/go/mod/modconfig"
)

var ignoringCUERegistryOnce sync.Once

// getRegistryResolver returns an implementation of [modregistry.Resolver]
// that resolves to registries as specified in the configuration.
//
// If external modules are disabled and there's no other issue,
// it returns (nil, nil).
func getRegistryResolver() (*modconfig.Resolver, error) {
	// TODO support registry configuration file too.
	env := os.Getenv("CUE_REGISTRY")
	if !cueexperiment.Flags.Modules {
		if env != "" {
			ignoringCUERegistryOnce.Do(func() {
				fmt.Fprintf(os.Stderr, "warning: ignoring CUE_REGISTRY because modules experiment is not enabled. Set CUE_EXPERIMENT=modules to enable it.\n")
			})
		}
		return nil, nil
	}
	return modconfig.NewResolver(&modconfig.Config{
		CUERegistry: env,
	})
}

func getCachedRegistry() (modload.Registry, error) {
	resolver, err := getRegistryResolver()
	if resolver == nil {
		return nil, err
	}
	cacheDir, err := cueconfig.CacheDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cacheDir, 0o777); err != nil {
		return nil, fmt.Errorf("cannot create cache directory: %v", err)
	}
	return modcache.New(modregistry.NewClientWithResolver(resolver), cacheDir)
}
