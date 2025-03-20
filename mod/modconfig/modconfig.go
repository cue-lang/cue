// Package modconfig provides access to the standard CUE
// module configuration, including registry access and authorization.
package modconfig

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"slices"
	"strings"
	"sync"

	"cuelabs.dev/go/oci/ociregistry"
	"cuelabs.dev/go/oci/ociregistry/ociauth"
	"cuelabs.dev/go/oci/ociregistry/ociclient"
	"golang.org/x/oauth2"

	"cuelang.org/go/internal/cueconfig"
	"cuelang.org/go/internal/cueversion"
	"cuelang.org/go/internal/mod/modload"
	"cuelang.org/go/internal/mod/modpkgload"
	"cuelang.org/go/internal/mod/modresolve"
	"cuelang.org/go/mod/modcache"
	"cuelang.org/go/mod/modregistry"
	"cuelang.org/go/mod/module"
)

// Registry is used to access CUE modules from external sources.
type Registry interface {
	// Requirements returns a list of the modules required by the given module
	// version.
	Requirements(ctx context.Context, m module.Version) ([]module.Version, error)

	// Fetch returns the location of the contents for the given module
	// version, downloading it if necessary.
	Fetch(ctx context.Context, m module.Version) (module.SourceLoc, error)

	// ModuleVersions returns all the versions for the module with the
	// given path, which should contain a major version.
	ModuleVersions(ctx context.Context, mpath string) ([]string, error)
}

// CachedRegistry is optionally implemented by a registry that
// contains a cache.
type CachedRegistry interface {
	// FetchFromCache looks up the given module in the cache.
	// It returns an error that satisfies [errors.Is]([modregistry.ErrNotFound]) if the
	// module is not present in the cache at this version or if there
	// is no cache.
	FetchFromCache(mv module.Version) (module.SourceLoc, error)
}

// We don't want to make modload part of the cue/load API,
// so we define the above type independently, but we want
// it to be interchangeable, so check that statically here.
var (
	_ Registry                  = modload.Registry(nil)
	_ modload.Registry          = Registry(nil)
	_ CachedRegistry            = modpkgload.CachedRegistry(nil)
	_ modpkgload.CachedRegistry = CachedRegistry(nil)
)

// DefaultRegistry is the default registry host.
const DefaultRegistry = "registry.cue.works"

// Resolver implements [modregistry.Resolver] in terms of the
// CUE registry configuration file and auth configuration.
type Resolver struct {
	resolver    modresolve.LocationResolver
	newRegistry func(host string, insecure bool) (ociregistry.Interface, error)

	mu         sync.Mutex
	registries map[string]ociregistry.Interface
}

// Config provides the starting point for the configuration.
type Config struct {
	// TODO allow for a custom resolver to be passed in.

	// Transport is used to make the underlying HTTP requests.
	// If it's nil, [http.DefaultTransport] will be used.
	Transport http.RoundTripper

	// Env provides environment variable values. If this is nil,
	// the current process's environment will be used.
	Env []string

	// CUERegistry specifies the registry or registries to use
	// to resolve modules. If it is empty, $CUE_REGISTRY
	// is used.
	// Experimental: this field might go away in a future version.
	CUERegistry string

	// ClientType is used as part of the User-Agent header
	// that's added in each outgoing HTTP request.
	// If it's empty, it defaults to "cuelang.org/go".
	ClientType string
}

// NewResolver returns an implementation of [modregistry.Resolver]
// that uses cfg to guide registry resolution. If cfg is nil, it's
// equivalent to passing pointer to a zero Config struct.
//
// It consults the same environment variables used by the
// cue command.
//
// The contents of the configuration will not be mutated.
func NewResolver(cfg *Config) (*Resolver, error) {
	cfg = newRef(cfg)
	cfg.Transport = cueversion.NewTransport(cfg.ClientType, cfg.Transport)
	getenv := getenvFunc(cfg.Env)
	var configData []byte
	var configPath string
	cueRegistry := cfg.CUERegistry
	if cueRegistry == "" {
		cueRegistry = getenv("CUE_REGISTRY")
	}
	kind, rest, _ := strings.Cut(cueRegistry, ":")
	switch kind {
	case "file":
		data, err := os.ReadFile(rest)
		if err != nil {
			return nil, err
		}
		configData, configPath = data, rest
	case "inline":
		configData, configPath = []byte(rest), "inline"
	case "simple":
		cueRegistry = rest
	}
	var resolver modresolve.LocationResolver
	var err error
	if configPath != "" {
		resolver, err = modresolve.ParseConfig(configData, configPath, DefaultRegistry)
	} else {
		resolver, err = modresolve.ParseCUERegistry(cueRegistry, DefaultRegistry)
	}
	if err != nil {
		return nil, fmt.Errorf("bad value for registry: %v", err)
	}
	return &Resolver{
		resolver: resolver,
		newRegistry: func(host string, insecure bool) (ociregistry.Interface, error) {
			return ociclient.New(host, &ociclient.Options{
				Insecure: insecure,
				Transport: &cueLoginsTransport{
					getenv: getenv,
					cfg:    cfg,
				},
			})
		},
		registries: make(map[string]ociregistry.Interface),
	}, nil
}

// Host represents a registry host name and whether
// it should be accessed via a secure connection or not.
type Host = modresolve.Host

// AllHosts returns all the registry hosts that the resolver might resolve to,
// ordered lexically by hostname.
func (r *Resolver) AllHosts() []Host {
	return r.resolver.AllHosts()
}

// HostLocation represents a registry host and a location with it.
type HostLocation = modresolve.Location

// ResolveToLocation returns the host location for the given module path and version
// without creating a Registry instance for it.
func (r *Resolver) ResolveToLocation(mpath string, version string) (HostLocation, bool) {
	return r.resolver.ResolveToLocation(mpath, version)
}

// ResolveToRegistry implements [modregistry.Resolver.ResolveToRegistry].
func (r *Resolver) ResolveToRegistry(mpath string, version string) (modregistry.RegistryLocation, error) {
	loc, ok := r.resolver.ResolveToLocation(mpath, version)
	if !ok {
		// This can happen when mpath is invalid, which should not
		// happen in practice, as the only caller is modregistry which
		// vets module paths before calling Resolve.
		//
		// It can also happen when the user has explicitly configured a "none"
		// registry to avoid falling back to a default registry.
		return modregistry.RegistryLocation{}, fmt.Errorf("cannot resolve %s (version %q) to registry: %w", mpath, version, modregistry.ErrRegistryNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	reg := r.registries[loc.Host]
	if reg == nil {
		reg1, err := r.newRegistry(loc.Host, loc.Insecure)
		if err != nil {
			return modregistry.RegistryLocation{}, fmt.Errorf("cannot make client: %v", err)
		}
		r.registries[loc.Host] = reg1
		reg = reg1
	}
	return modregistry.RegistryLocation{
		Registry:   reg,
		Repository: loc.Repository,
		Tag:        loc.Tag,
	}, nil
}

// cueLoginsTransport implements [http.RoundTripper] by using
// tokens from the CUE login information when available, falling
// back to using the standard [ociauth] transport implementation.
type cueLoginsTransport struct {
	cfg    *Config
	getenv func(string) string

	// initOnce guards initErr, logins, and transport.
	initOnce sync.Once
	initErr  error
	// loginsMu guards the logins pointer below.
	// Note that an instance of cueconfig.Logins is read-only and
	// does not have to be guarded.
	loginsMu sync.Mutex
	logins   *cueconfig.Logins
	// transport holds the underlying transport. This wraps
	// t.cfg.Transport.
	transport http.RoundTripper

	// mu guards the fields below.
	mu sync.Mutex

	// cachedTransports holds a transport per host.
	// This is needed because the oauth2 API requires a
	// different client for each host. Each of these transports
	// wraps the transport above.
	cachedTransports map[string]http.RoundTripper
}

func (t *cueLoginsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Return an error lazily on the first request because if the
	// user isn't doing anything that requires a registry, we
	// shouldn't complain about reading a bad configuration file.
	if err := t.init(); err != nil {
		return nil, err
	}

	t.loginsMu.Lock()
	logins := t.logins
	t.loginsMu.Unlock()

	if logins == nil {
		return t.transport.RoundTrip(req)
	}
	// TODO: note that a CUE registry may include a path prefix,
	// so using solely the host will not work with such a path.
	// Can we do better here, perhaps keeping the path prefix up to "/v2/"?
	host := req.URL.Host
	login, ok := logins.Registries[host]
	if !ok {
		return t.transport.RoundTrip(req)
	}

	t.mu.Lock()
	transport := t.cachedTransports[host]
	if transport == nil {
		tok := cueconfig.TokenFromLogin(login)
		oauthCfg := cueconfig.RegistryOAuthConfig(Host{
			Name:     host,
			Insecure: req.URL.Scheme == "http",
		})

		// Make the oauth client use the transport that was set up
		// in init.
		ctx := context.WithValue(req.Context(), oauth2.HTTPClient, &http.Client{
			Transport: t.transport,
		})
		transport = oauth2.NewClient(ctx,
			&cachingTokenSource{
				updateFunc: func(tok *oauth2.Token) error {
					return t.updateLogin(host, tok)
				},
				base: oauthCfg.TokenSource(ctx, tok),
				t:    tok,
			},
		).Transport
		t.cachedTransports[host] = transport
	}
	// Unlock immediately so we don't hold the lock for the entire
	// request, which would preclude any concurrency when
	// making HTTP requests.
	t.mu.Unlock()
	return transport.RoundTrip(req)
}

func (t *cueLoginsTransport) updateLogin(host string, new *oauth2.Token) error {
	// Reload the logins file in case another process changed it in the meantime.
	loginsPath, err := cueconfig.LoginConfigPath(t.getenv)
	if err != nil {
		// TODO: this should never fail. Log a warning.
		return nil
	}

	// Lock the logins for the entire duration of the update to avoid races
	t.loginsMu.Lock()
	defer t.loginsMu.Unlock()

	logins, err := cueconfig.UpdateRegistryLogin(loginsPath, host, new)
	if err != nil {
		return err
	}

	t.logins = logins

	return nil
}

func (t *cueLoginsTransport) init() error {
	t.initOnce.Do(func() {
		t.initErr = t._init()
	})
	return t.initErr
}

func (t *cueLoginsTransport) _init() error {
	// If a registry was authenticated via `cue login`, use that.
	// If not, fall back to authentication via Docker's config.json.
	// Note that the order below is backwards, since we layer interfaces.

	config, err := ociauth.LoadWithEnv(nil, t.cfg.Env)
	if err != nil {
		return fmt.Errorf("cannot load OCI auth configuration: %v", err)
	}
	t.transport = ociauth.NewStdTransport(ociauth.StdTransportParams{
		Config:    config,
		Transport: t.cfg.Transport,
	})

	// If we can't locate a logins.json file at all, then we'll continue.
	// We only refuse to continue if we find an invalid logins.json file.
	loginsPath, err := cueconfig.LoginConfigPath(t.getenv)
	if err != nil {
		// TODO: this should never fail. Log a warning.
		return nil
	}
	logins, err := cueconfig.ReadLogins(loginsPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("cannot load CUE registry logins: %v", err)
	}
	t.logins = logins
	t.cachedTransports = make(map[string]http.RoundTripper)
	return nil
}

// NewRegistry returns an implementation of the Registry
// interface suitable for passing to [load.Instances].
// It uses the standard CUE cache directory.
func NewRegistry(cfg *Config) (Registry, error) {
	cfg = newRef(cfg)
	resolver, err := NewResolver(cfg)
	if err != nil {
		return nil, err
	}
	cacheDir, err := cueconfig.CacheDir(getenvFunc(cfg.Env))
	if err != nil {
		return nil, err
	}
	return modcache.New(modregistry.NewClientWithResolver(resolver), cacheDir)
}

func getenvFunc(env []string) func(string) string {
	if env == nil {
		return os.Getenv
	}
	return func(key string) string {
		for _, e := range slices.Backward(env) {
			if len(e) >= len(key)+1 && e[len(key)] == '=' && e[:len(key)] == key {
				return e[len(key)+1:]
			}
		}
		return ""
	}
}

func newRef[T any](x *T) *T {
	var x1 T
	if x != nil {
		x1 = *x
	}
	return &x1
}

// cachingTokenSource works similar to oauth2.ReuseTokenSource, except that it
// also exposes a hook to get a hold of the refreshed token, so that it can be
// stored in persistent storage.
type cachingTokenSource struct {
	updateFunc func(tok *oauth2.Token) error
	base       oauth2.TokenSource // called when t is expired

	mu sync.Mutex // guards t
	t  *oauth2.Token
}

func (s *cachingTokenSource) Token() (*oauth2.Token, error) {
	s.mu.Lock()
	t := s.t

	if t.Valid() {
		s.mu.Unlock()
		return t, nil
	}

	t, err := s.base.Token()
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}

	s.t = t
	s.mu.Unlock()

	err = s.updateFunc(t)
	if err != nil {
		return nil, err
	}

	return t, nil
}
