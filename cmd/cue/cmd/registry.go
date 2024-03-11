package cmd

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"

	"cuelang.org/go/internal/cuedebug"
	"cuelang.org/go/internal/cueexperiment"
	"cuelang.org/go/internal/cueversion"
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
	return modconfig.NewResolver(newModConfig())
}

func getCachedRegistry() (modload.Registry, error) {
	if !modulesExperimentEnabled() {
		return nil, nil
	}
	return modconfig.NewRegistry(newModConfig())
}

func newModConfig() *modconfig.Config {
	return &modconfig.Config{
		Transport: httpTransport(),
	}
}

func httpTransport() http.RoundTripper {
	transport := http.DefaultTransport
	if cuedebug.Flags.HTTP {
		transport = httplog.Transport(&httplog.TransportConfig{
			// It would be nice to use the default slog logger,
			// but that does a terrible job of printing structured
			// values, so use JSON output instead.
			Logger: httplog.SlogLogger{
				Logger: slog.New(slog.NewJSONHandler(os.Stderr, nil)),
			},
		})
	}
	// Always add a User-Agent header.
	return cueversion.NewTransport("cmd/cue", transport)
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
