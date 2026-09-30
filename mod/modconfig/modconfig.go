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
	"time"

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
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/modregistry"
	"cuelang.org/go/mod/module"
)

// Registry is used to access CUE modules from external sources.
type Registry interface {
	// ModFile returns the module file for the given module version.
	// The caller must not mutate the returned value.
	ModFile(ctx context.Context, mv module.Version) (*modfile.File, error)

	// Fetch returns the location of the contents for the given module
	// version, downloading it if necessary.
	Fetch(ctx context.Context, m module.Version) (module.SourceLoc, error)

	// ModuleVersions returns all the versions for the module with the
	// given path, which should contain a major version.
	ModuleVersions(ctx context.Context, mpath string) ([]string, error)
}

// CachedRegistry is optionally implemented by a [Registry] that
// contains a cache.
type CachedRegistry interface {
	Registry

	// FetchFromCache looks up the given module in the cache.
	// It returns an error that satisfies [errors.Is]([modregistry.ErrNotFound]) if the
	// module is not present in the cache at this version or if there
	// is no cache.
	FetchFromCache(mv module.Version) (module.SourceLoc, error)
}

var (
	// We don't want to make modload part of the cue/load API,
	// so we define the above type independently, but we want
	// it to be interchangeable, so check that statically here.
	_ Registry         = modload.Registry(nil)
	_ modload.Registry = Registry(nil)

	// [modpkgload.CachedRegistry] is a subset of [CachedRegistry].
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
			transport := &cueLoginsTransport{
				getenv: getenv,
				cfg:    cfg,
			}
			// Initialize the transport eagerly so that a broken auth
			// configuration surfaces as a direct error rather than
			// wrapped in the HTTP request that would first use it.
			if err := transport.init(); err != nil {
				return nil, err
			}
			return ociclient.New(host, &ociclient.Options{
				Insecure:  insecure,
				Transport: transport,
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
			return modregistry.RegistryLocation{}, err
		}
		r.registries[loc.Host] = reg1
		reg = reg1
	}
	return modregistry.RegistryLocation{
		Registry:   reg,
		Host:       loc.Host,
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

	// cachedTransports holds a transport per host, as each host
	// has its own token. Each of them wraps the transport above.
	cachedTransports map[string]*loginTransport
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
		transport = &loginTransport{
			base: t.transport,
			oauthCfg: cueconfig.RegistryOAuthConfig(Host{
				Name:     host,
				Insecure: req.URL.Scheme == "http",
			}),
			updateFunc: func(tok *oauth2.Token) error {
				return t.updateLogin(host, tok)
			},
			tok: cueconfig.TokenFromLogin(login),
		}
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
	t.cachedTransports = make(map[string]*loginTransport)
	return nil
}

// NewRegistry returns an implementation of the [CachedRegistry]
// interface suitable for passing to [load.Instances].
// It uses the standard CUE cache directory.
func NewRegistry(cfg *Config) (CachedRegistry, error) {
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

// LazyRegistry implements [CachedRegistry] such that any configuration or setup errors,
// such as an invalid `CUE_REGISTRY` value or an inaccessible `CUE_CACHE_DIR` directory
// are only surfaced as fatal errors for the user once the registry is actually needed.
//
// Notably, interacting with a CUE registry or the CUE disk caches is not required for many
// simple scenarios, such as loading and evaluating local files or modules
// without any external module dependencies.
type LazyRegistry struct {
	// New constructs the underlying registry. It is called at most once,
	// the first time any of the registry's methods is used.
	New func() (CachedRegistry, error)

	once    sync.Once
	onceReg CachedRegistry
	onceErr error
}

var _ CachedRegistry = (*LazyRegistry)(nil)

func (r *LazyRegistry) registry() (CachedRegistry, error) {
	r.once.Do(func() {
		r.onceReg, r.onceErr = r.New()
	})
	return r.onceReg, r.onceErr
}

func (r *LazyRegistry) ModFile(ctx context.Context, m module.Version) (*modfile.File, error) {
	reg, err := r.registry()
	if err != nil {
		return nil, err
	}
	return reg.ModFile(ctx, m)
}

func (r *LazyRegistry) Fetch(ctx context.Context, m module.Version) (module.SourceLoc, error) {
	reg, err := r.registry()
	if err != nil {
		return module.SourceLoc{}, err
	}
	return reg.Fetch(ctx, m)
}

func (r *LazyRegistry) FetchFromCache(m module.Version) (module.SourceLoc, error) {
	reg, err := r.registry()
	if err != nil {
		return module.SourceLoc{}, err
	}
	return reg.FetchFromCache(m)
}

func (r *LazyRegistry) ModuleVersions(ctx context.Context, mpath string) ([]string, error) {
	reg, err := r.registry()
	if err != nil {
		return nil, err
	}
	return reg.ModuleVersions(ctx, mpath)
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

// tokenRefreshTimeout bounds each refresh of an OAuth token.
const tokenRefreshTimeout = time.Minute

// loginTransport implements [http.RoundTripper] for a single registry host
// by authorizing each request with the token from the CUE login information.
// It works like [oauth2.Transport] with an [oauth2.ReuseTokenSource],
// except that each refresh of the token uses the context of the request
// which needs it, and that the refreshed token is stored via updateFunc.
//
// TODO: use [oauth2.Transport] again once its token sources take a context
// per call; see https://github.com/golang/oauth2/issues/262.
type loginTransport struct {
	base       http.RoundTripper // also used to refresh the token
	oauthCfg   oauth2.Config
	updateFunc func(tok *oauth2.Token) error

	mu      sync.Mutex // guards the fields below
	tok     *oauth2.Token
	refresh *tokenRefresh // the refresh in flight, if any
}

// tokenRefresh is a refresh of an expired token,
// shared by all the requests which need the new token.
type tokenRefresh struct {
	done chan struct{} // closed once tok and err are set
	tok  *oauth2.Token
	err  error
}

func (t *loginTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	tok, err := t.token(req.Context())
	if err != nil {
		if req.Body != nil {
			req.Body.Close()
		}
		return nil, err
	}
	req = req.Clone(req.Context())
	tok.SetAuthHeader(req)
	return t.base.RoundTrip(req)
}

// token returns the current token, refreshing it first if it has expired.
func (t *loginTransport) token(ctx context.Context) (*oauth2.Token, error) {
	// A canceled request must not start a refresh, which would outlive it.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	tok, r, start := t.tokenOrRefresh()
	if r == nil {
		return tok, nil
	}
	if start {
		// Run the refresh here rather than in the background, so that
		// the new token is stored by the time the request returns.
		t.runRefresh(ctx, r, tok)
		return r.tok, r.err
	}
	select {
	case <-r.done:
		return r.tok, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// tokenOrRefresh returns the current token and, if it has expired,
// the refresh which replaces it. If no refresh was in flight, a new one
// is returned along with start set to true, and the caller must run it.
func (t *loginTransport) tokenOrRefresh() (tok *oauth2.Token, r *tokenRefresh, start bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.tok.Valid() {
		return t.tok, nil, false
	}
	if t.refresh == nil {
		t.refresh = &tokenRefresh{done: make(chan struct{})}
		start = true
	}
	return t.tok, t.refresh, start
}

// runRefresh runs the refresh r of the expired token old.
func (t *loginTransport) runRefresh(ctx context.Context, r *tokenRefresh, old *oauth2.Token) {
	// Abandoning a refresh halfway can lose a rotated refresh token and
	// force a new login, so ignore cancellation and use a timeout instead.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), tokenRefreshTimeout)
	defer cancel()
	ctx = context.WithValue(ctx, oauth2.HTTPClient, &http.Client{
		Transport: t.base,
	})
	tok, err := t.oauthCfg.TokenSource(ctx, old).Token()
	if err == nil {
		// Store the token before the next refresh can start,
		// so that an older token never overwrites a newer one.
		err = t.updateFunc(tok)
	}

	t.mu.Lock()
	defer t.mu.Unlock()
	if tok != nil {
		// Keep using the new token even if it could not be stored.
		t.tok = tok
	}
	r.tok, r.err = tok, err
	t.refresh = nil
	close(r.done)
}
