package cmd

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"

	"cuelabs.dev/go/oci/ociregistry"
	"cuelabs.dev/go/oci/ociregistry/ociauth"
	"cuelabs.dev/go/oci/ociregistry/ociclient"

	"cuelang.org/go/internal/cueexperiment"
	"cuelang.org/go/internal/mod/modcache"
	"cuelang.org/go/internal/mod/modload"
	"cuelang.org/go/internal/mod/modmux"
	"cuelang.org/go/internal/mod/modresolve"
)

// getRegistry returns the registry to pull modules from.
// If external modules are disabled and there's no other issue,
// it returns (nil, nil).
func getRegistry() (ociregistry.Interface, error) {
	// TODO document CUE_REGISTRY via a new "cue help environment" subcommand.
	env := os.Getenv("CUE_REGISTRY")
	if !cueexperiment.Flags.Modules {
		if env != "" {
			fmt.Fprintf(os.Stderr, "warning: ignoring CUE_REGISTRY because modules experiment is not enabled. Set CUE_EXPERIMENT=modules to enable it.\n")
		}
		return nil, nil
	}
	resolver, err := modresolve.ParseCUERegistry(env, "registry.cue.works")
	if err != nil {
		return nil, fmt.Errorf("bad value for $CUE_REGISTRY: %v", err)
	}
	// If the user isn't doing anything that requires a registry, we
	// shouldn't complain about reading a bad configuration file,
	// so check only when required.
	authOnce := sync.OnceValues(func() (ociauth.Authorizer, error) {
		// If a registry was authenticated via `cue login`, use that.
		// If not, fall back to authentication via Docker's config.json.
		// Note that the order below is backwards, since we layer interfaces.

		config, err := ociauth.Load(nil)
		if err != nil {
			return nil, fmt.Errorf("cannot load OCI auth configuration: %v", err)
		}
		var auth ociauth.Authorizer = ociauth.NewStdAuthorizer(ociauth.StdAuthorizerParams{
			Config: config,
		})

		loginsPath, err := findLoginsPath()
		if err != nil {
			return nil, fmt.Errorf("cannot find the path to store CUE registry logins: %v", err)
		}
		logins, err := readLogins(loginsPath)
		if err != nil {
			return nil, fmt.Errorf("cannot load CUE registry logins: %v", err)
		}
		auth = &cueLoginsAuthorizer{
			logins:        logins,
			cachedClients: make(map[string]*http.Client),
			next:          auth,
		}
		return auth, nil
	})

	return modmux.New(resolver, func(host string, insecure bool) (ociregistry.Interface, error) {
		auth, err := authOnce()
		if err != nil {
			return nil, err
		}
		return ociclient.New(host, &ociclient.Options{
			Insecure:   insecure,
			Authorizer: auth,
		})
	}), nil
}

type cueLoginsAuthorizer struct {
	logins *cueLogins
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
		tok := tokenFromLogin(login)
		oauthCfg := registryOAuthConfig(host)
		// TODO: When this client refreshes an access token,
		// we should store the refreshed token on disk.
		client = oauthCfg.Client(context.Background(), tok)
		a.cachedClients[host] = client
	}
	a.mu.Unlock()
	return client.Do(req)
}

func getCachedRegistry() (modload.Registry, error) {
	reg, err := getRegistry()
	if reg == nil {
		return nil, err
	}
	cacheDir, err := modCacheDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cacheDir, 0o777); err != nil {
		return nil, fmt.Errorf("cannot create cache directory: %v", err)
	}
	return modcache.New(reg, cacheDir)
}
