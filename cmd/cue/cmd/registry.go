package cmd

import (
	"fmt"
	"log/slog"
	"maps"
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
	if !cuedebug.Flags.HTTP {
		return newUserAgentTransport(cueversion.UserAgent("cmd/cue"), http.DefaultTransport)
	}
	return httplog.Transport(&httplog.TransportConfig{
		// It would be nice to use the default slog logger,
		// but that does a terrible job of printing structured
		// values, so use JSON output instead.
		Logger: httplog.SlogLogger{
			Logger: slog.New(slog.NewJSONHandler(os.Stderr, nil)),
		},
	})
}

func newUserAgentTransport(userAgent string, t http.RoundTripper) http.RoundTripper {
	if userAgent == "" {
		return t
	}
	return &userAgentTransport{
		transport: t,
		userAgent: userAgent,
	}
}

type userAgentTransport struct {
	transport http.RoundTripper
	userAgent string
}

func (t *userAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Don't override the user agent if it's already set explicitly.
	if req.UserAgent() != "" {
		return t.transport.RoundTrip(req)
	}

	// RoundTrip isn't allowed to modify the request, but we
	// can avoid doing a full clone.
	req1 := *req
	req1.Header = maps.Clone(req.Header)
	req1.Header.Set("User-Agent", t.userAgent)
	return t.transport.RoundTrip(&req1)
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
