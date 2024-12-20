package cmd

import (
	"log/slog"
	"net/http"
	"os"

	"cuelang.org/go/internal/cuedebug"
	"cuelang.org/go/internal/httplog"
	"cuelang.org/go/internal/mod/modload"
	"cuelang.org/go/mod/modconfig"
)

// getRegistryResolver returns an implementation of [modregistry.Resolver]
// that resolves to registries as specified in the configuration.
func getRegistryResolver() (*modconfig.Resolver, error) {
	cfg, err := newModConfig()
	if err != nil {
		return nil, err
	}
	return modconfig.NewResolver(cfg)
}

func getCachedRegistry() (modload.Registry, error) {
	cfg, err := newModConfig()
	if err != nil {
		return nil, err
	}
	return modconfig.NewRegistry(cfg)
}

func newModConfig() (*modconfig.Config, error) {
	transport, err := httpTransport()
	if err != nil {
		return nil, err
	}
	return &modconfig.Config{
		Transport:  transport,
		ClientType: "cmd/cue",
	}, nil
}

func httpTransport() (http.RoundTripper, error) {
	debug, err := cuedebug.Flags()
	if err != nil {
		return nil, err
	}
	if !debug.HTTP {
		return http.DefaultTransport, nil
	}
	return httplog.Transport(&httplog.TransportConfig{
		// It would be nice to use the default slog logger,
		// but that does a terrible job of printing structured
		// values, so use JSON output instead.
		Logger: httplog.SlogLogger{
			Logger: slog.New(slog.NewJSONHandler(os.Stderr, nil)),
		},
	}), nil
}
