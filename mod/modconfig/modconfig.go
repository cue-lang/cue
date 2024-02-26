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
	"strings"
	"sync"

	"cuelabs.dev/go/oci/ociregistry"
	"cuelabs.dev/go/oci/ociregistry/ociauth"
	"cuelabs.dev/go/oci/ociregistry/ociclient"

	"cuelang.org/go/internal/cueconfig"
	"cuelang.org/go/internal/mod/modload"
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

// We don't want to make modload part of the cue/load API,
// so we define the above type independently, but we want
// it to be interchangeable, so check that statically here.
var (
	_ Registry         = modload.Registry(nil)
	_ modload.Registry = Registry(nil)
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

	// Authorizer is used to authorize registry requests.
	// If it's nil, a default authorizer will be used that consults
	// the information stored by "cue login" and "docker login".
	Authorizer ociauth.Authorizer
}

// NewResolver returns an implementation of [modregistry.Resolver]
// that uses cfg to guide registry resolution. If cfg is nil, it's
// equivalent to passing pointer to a zero Config struct.
//
// It consults the same environment variables used by the
// cue command.
//
// The contents of the configuration will not be mutated.
func NewResolver(cfg0 *Config) (*Resolver, error) {
	var cfg Config
	if cfg0 != nil {
		cfg = *cfg0
	}
	var configData []byte
	var configPath string
	cueRegistry := os.Getenv("CUE_REGISTRY")
	kind, rest, _ := strings.Cut(cueRegistry, ":")
	switch kind {
	case "file":
		data, err := os.ReadFile(rest)
		if err != nil {
			return nil, err
		}
		configData, configPath = data, rest
	case "inline":
		configData, configPath = []byte(rest), "$CUE_REGISTRY"
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
		return nil, fmt.Errorf("bad value for $CUE_REGISTRY: %v", err)
	}
	// If the user isn't doing anything that requires a registry, we
	// shouldn't complain about reading a bad configuration file,
	// so check only when required.
	authOnce := sync.OnceValues(func() (ociauth.Authorizer, error) {
		if cfg.Authorizer != nil {
			return cfg.Authorizer, nil
		}
		// If a registry was authenticated via `cue login`, use that.
		// If not, fall back to authentication via Docker's config.json.
		// Note that the order below is backwards, since we layer interfaces.

		config, err := ociauth.Load(nil)
		if err != nil {
			return nil, fmt.Errorf("cannot load OCI auth configuration: %v", err)
		}
		auth := ociauth.NewStdAuthorizer(ociauth.StdAuthorizerParams{
			Config: config,
		})

		// If we can't locate a logins.json file at all, skip cueLoginsAuthorizer entirely.
		// We only refuse to continue if we find an invalid logins.json file.
		loginsPath, err := cueconfig.LoginConfigPath()
		if err != nil {
			return auth, nil
		}
		logins, err := cueconfig.ReadLogins(loginsPath)
		if errors.Is(err, fs.ErrNotExist) {
			return auth, nil
		}
		if err != nil {
			return nil, fmt.Errorf("cannot load CUE registry logins: %v", err)
		}
		return &cueLoginsAuthorizer{
			logins:        logins,
			cachedClients: make(map[string]*http.Client),
			next:          auth,
		}, nil
	})

	newRegistry := func(host string, insecure bool) (ociregistry.Interface, error) {
		auth, err := authOnce()
		if err != nil {
			return nil, err
		}
		return ociclient.New(host, &ociclient.Options{
			Insecure:   insecure,
			Authorizer: auth,
		})
	}
	return &Resolver{
		resolver:    resolver,
		newRegistry: newRegistry,
		registries:  make(map[string]ociregistry.Interface),
	}, nil
}

// AllHosts returns information on all the registry host names referred to
// by the resolver.
func (r *Resolver) AllHosts() []string {
	allHosts := r.resolver.AllHosts()
	names := make([]string, len(allHosts))
	for i, h := range allHosts {
		names[i] = h.Name
	}
	return names
}

// HostLocation represents a registry host and a location with it.
type HostLocation = modresolve.Location

// ResolveToLocation returns the host location for the given module path and version
// without creating a Registry instance for it.
func (r *Resolver) ResolveToLocation(mpath string, vers string) (HostLocation, bool) {
	return r.resolver.ResolveToLocation(mpath, vers)
}

// Resolve implements modregistry.Resolver.Resolve.
func (r *Resolver) ResolveToRegistry(mpath string, vers string) (modregistry.RegistryLocation, error) {
	loc, ok := r.resolver.ResolveToLocation(mpath, vers)
	if !ok {
		// This can only happen when mpath is invalid, which should not
		// happen in practice, as the only caller is modregistry which
		// vets module paths before calling Resolve.
		return modregistry.RegistryLocation{}, fmt.Errorf("cannot resolve %s (version %s) to registry", mpath, vers)
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

type cueLoginsAuthorizer struct {
	logins *cueconfig.Logins
	next   ociauth.Authorizer
	// mu guards the fields below.
	mu            sync.Mutex
	cachedClients map[string]*http.Client
}

func (a *cueLoginsAuthorizer) DoRequest(req *http.Request, requiredScope ociauth.Scope) (*http.Response, error) {
	// TODO: note that a CUE registry may include a path prefix,
	// so using solely the host will not work with such a path.
	// Can we do better here, perhaps keeping the path prefix up to "/v2/"?
	host := req.URL.Host
	login, ok := a.logins.Registries[host]
	if !ok {
		return a.next.DoRequest(req, requiredScope)
	}

	a.mu.Lock()
	client := a.cachedClients[host]
	if client == nil {
		tok := cueconfig.TokenFromLogin(login)
		oauthCfg := cueconfig.RegistryOAuthConfig(host)
		// TODO: When this client refreshes an access token,
		// we should store the refreshed token on disk.
		client = oauthCfg.Client(context.Background(), tok)
		a.cachedClients[host] = client
	}
	// Unlock immediately so we don't hold the lock for the entire
	// request, which would preclude any concurrency when
	// making HTTP requests.
	a.mu.Unlock()
	return client.Do(req)
}

// NewRegistry returns an implementation of the Registry
// interface suitable for passing to [load.Instances].
// It uses the standard CUE cache directory.
func NewRegistry(cfg *Config) (Registry, error) {
	resolver, err := NewResolver(cfg)
	if err != nil {
		return nil, err
	}
	cacheDir, err := cueconfig.CacheDir()
	if err != nil {
		return nil, err
	}
	return modcache.New(modregistry.NewClientWithResolver(resolver), cacheDir)
}
