package cmd

import (
	"fmt"
	"net/http"
	"os"
	"sync"

	"cuelang.org/go/internal/cueexperiment"
	"cuelang.org/go/internal/httplog"
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
	var transport http.RoundTripper
	if httpLogging {
		transport = httplog.Transport(nil)
	}
	return modconfig.NewResolver(&modconfig.Config{
		Transport: transport,
	})
}

const httpLogging = true

func getCachedRegistry() (modload.Registry, error) {
	if !modulesExperimentEnabled() {
		return nil, nil
	}
	var transport http.RoundTripper
	if httpLogging {
		transport = httplog.Transport(nil, nil)
	}
	return modconfig.NewRegistry(&modconfig.Config{
		Transport: transport,
	})
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
