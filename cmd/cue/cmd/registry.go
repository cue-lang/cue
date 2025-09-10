package cmd

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"sync"

	"cuelang.org/go/internal/cuedebug"
	"cuelang.org/go/internal/httplog"
	"cuelang.org/go/mod/modconfig"
	"cuelang.org/go/mod/module"
)

// getRegistryResolver returns an implementation of [modregistry.Resolver]
// that resolves to registries as specified in the configuration.
func getRegistryResolver() (*modconfig.Resolver, error) {
	return modconfig.NewResolver(newModConfig(""))
}

func getRegistry() (modconfig.CachedRegistry, error) {
	return modconfig.NewRegistry(newModConfig(""))
}

func getLazyRegistry() modconfig.CachedRegistry {
	return modconfig.NewLazyRegistry(newModConfig(""))
}

func newModConfig(registry string) *modconfig.Config {
	return &modconfig.Config{
		Transport:   httpTransport(),
		ClientType:  "cmd/cue",
		CUERegistry: registry,
	}
}

var _ modconfig.CachedRegistry = (*lazyRegistry)(nil)

// lazyRegistry implements [modconfig.CachedRegistry] by returning err from all methods.
type lazyRegistry struct {
	fn func() (modconfig.CachedRegistry, error)

	once    sync.Once
	onceReg modconfig.CachedRegistry
	onceErr error
}

func (r *lazyRegistry) registry() (modconfig.CachedRegistry, error) {
	r.once.Do(func() {
		r.onceReg, r.onceErr = r.fn()
	})
	return r.onceReg, r.onceErr
}

func (r *lazyRegistry) Requirements(ctx context.Context, m module.Version) ([]module.Version, error) {
	reg, err := r.registry()
	if err != nil {
		return nil, err
	}
	return reg.Requirements(ctx, m)
}

func (r *lazyRegistry) Fetch(ctx context.Context, m module.Version) (module.SourceLoc, error) {
	reg, err := r.registry()
	if err != nil {
		return module.SourceLoc{}, err
	}
	return reg.Fetch(ctx, m)
}

func (r *lazyRegistry) FetchFromCache(m module.Version) (module.SourceLoc, error) {
	reg, err := r.registry()
	if err != nil {
		return module.SourceLoc{}, err
	}
	return reg.FetchFromCache(m)
}

func (r *lazyRegistry) ModuleVersions(ctx context.Context, mpath string) ([]string, error) {
	reg, err := r.registry()
	if err != nil {
		return nil, err
	}
	return reg.ModuleVersions(ctx, mpath)
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
