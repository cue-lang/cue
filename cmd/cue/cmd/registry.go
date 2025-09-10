package cmd

import (
	"log/slog"
	"net/http"
	"os"

	"cuelang.org/go/internal/cuedebug"
	"cuelang.org/go/internal/httplog"
	"cuelang.org/go/mod/modconfig"
)

// getRegistryResolver returns an implementation of [modregistry.Resolver]
// that resolves to registries as specified in the configuration.
func getRegistryResolver() (*modconfig.Resolver, error) {
	return modconfig.NewResolver(newModConfig(""))
}

// getRegistry sets up a registry eagerly, surfacing any configuration
// errors immediately. Use it for commands which always need the registry.
func getRegistry() (modconfig.CachedRegistry, error) {
	return modconfig.NewRegistry(newModConfig(""))
}

// getLazyRegistry sets up a registry lazily, deferring any configuration
// errors until the registry is actually used. Use it for commands which
// may not need to interact with a registry at all, such as when loading
// and evaluating local files or modules without external dependencies.
func getLazyRegistry() *modconfig.LazyRegistry {
	cfg := newModConfig("")
	return &modconfig.LazyRegistry{New: func() (modconfig.CachedRegistry, error) {
		return modconfig.NewRegistry(cfg)
	}}
}

func newModConfig(registry string) *modconfig.Config {
	return &modconfig.Config{
		Transport:   httpTransport(),
		ClientType:  "cmd/cue",
		CUERegistry: registry,
	}
}

func httpTransport() http.RoundTripper {
	cuedebug.Init()
	if !cuedebug.Flags.HTTP {
		return http.DefaultTransport
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
