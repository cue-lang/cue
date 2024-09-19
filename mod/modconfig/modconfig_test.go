package modconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cuelabs.dev/go/oci/ociregistry/ocimem"
	"cuelabs.dev/go/oci/ociregistry/ociserver"
	"github.com/go-quicktest/qt"
	"golang.org/x/oauth2"
	"golang.org/x/sync/errgroup"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/internal/cueconfig"
	"cuelang.org/go/internal/cueversion"
	"cuelang.org/go/internal/registrytest"
	"cuelang.org/go/mod/modcache"
	"cuelang.org/go/mod/module"
)

// TODO: the test below acts as a smoke test for the functionality here,
// but more of the behavior is tested in the cmd/cue script tests.
// We should do more of it here too.

func TestNewRegistry(t *testing.T) {
	modules := txtar.Parse([]byte(`
-- r1/foo.example_v0.0.1/cue.mod/module.cue --
module: "foo.example@v0"
language: version: "v0.8.0"
deps: "bar.example@v0": v: "v0.0.1"
-- r1/foo.example_v0.0.1/bar/bar.cue --
package bar
-- r1/bar.example_v0.0.1/cue.mod/module.cue --
module: "bar.example@v0"
language: version: "v0.8.0"
-- r1/bar.example_v0.0.1/y/y.cue --
package y

-- r2/auth.json --
{
	"username": "bob",
	"password": "somePassword"
}
-- r2/bar.example_v0.0.1/cue.mod/module.cue --
module: "bar.example@v0"
language: version: "v0.8.0"
-- r2/bar.example_v0.0.1/x/x.cue --
package x
`))
	fsys, err := txtar.FS(modules)
	qt.Assert(t, qt.IsNil(err))
	r1fs, err := fs.Sub(fsys, "r1")
	qt.Assert(t, qt.IsNil(err))
	r1, err := registrytest.New(r1fs, "")
	qt.Assert(t, qt.IsNil(err))
	r2fs, err := fs.Sub(fsys, "r2")
	qt.Assert(t, qt.IsNil(err))
	r2, err := registrytest.New(r2fs, "")
	qt.Assert(t, qt.IsNil(err))

	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)
	dockerCfg, err := json.Marshal(dockerConfig{
		Auths: map[string]authConfig{
			r2.Host(): {
				Username: "bob",
				Password: "somePassword",
			},
		},
	})
	qt.Assert(t, qt.IsNil(err))
	err = os.WriteFile(filepath.Join(dir, "config.json"), dockerCfg, 0o666)
	qt.Assert(t, qt.IsNil(err))

	t.Setenv("CUE_REGISTRY",
		fmt.Sprintf("foo.example=%s+insecure,%s+insecure",
			r1.Host(),
			r2.Host(),
		))
	cacheDir := filepath.Join(dir, "cache")
	t.Setenv("CUE_CACHE_DIR", cacheDir)
	t.Cleanup(func() {
		modcache.RemoveAll(cacheDir)
	})

	var transportInvoked atomic.Bool
	r, err := NewRegistry(&Config{
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			transportInvoked.Store(true)
			return http.DefaultTransport.RoundTrip(req)
		}),
	})
	qt.Assert(t, qt.IsNil(err))
	ctx := context.Background()
	gotRequirements, err := r.Requirements(ctx, module.MustNewVersion("foo.example@v0", "v0.0.1"))
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(gotRequirements, []module.Version{
		module.MustNewVersion("bar.example@v0", "v0.0.1"),
	}))

	loc, err := r.Fetch(ctx, module.MustNewVersion("bar.example@v0", "v0.0.1"))
	qt.Assert(t, qt.IsNil(err))
	data, err := fs.ReadFile(loc.FS, path.Join(loc.Dir, "x/x.cue"))
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(string(data), "package x\n"))

	// Check that we can make a Resolver with the same configuration.
	resolver, err := NewResolver(nil)
	qt.Assert(t, qt.IsNil(err))
	gotAllHosts := resolver.AllHosts()
	wantAllHosts := []Host{{Name: r1.Host(), Insecure: true}, {Name: r2.Host(), Insecure: true}}

	byHostname := func(a, b Host) int { return strings.Compare(a.Name, b.Name) }
	slices.SortFunc(gotAllHosts, byHostname)
	slices.SortFunc(wantAllHosts, byHostname)

	qt.Assert(t, qt.DeepEquals(gotAllHosts, wantAllHosts))

	// Check that the underlying custom transport was used.
	qt.Assert(t, qt.IsTrue(transportInvoked.Load()))
}

func TestDefaultTransportSetsUserAgent(t *testing.T) {
	// This test also checks that providing a nil Config.Transport
	// does the right thing.

	regFS, err := txtar.FS(txtar.Parse([]byte(`
-- bar.example_v0.0.1/cue.mod/module.cue --
module: "bar.example@v0"
language: version: "v0.8.0"
-- bar.example_v0.0.1/x/x.cue --
package x
`)))
	qt.Assert(t, qt.IsNil(err))
	ctx := context.Background()
	rmem := ocimem.NewWithConfig(&ocimem.Config{ImmutableTags: true})
	err = registrytest.Upload(ctx, rmem, regFS)
	qt.Assert(t, qt.IsNil(err))
	rh := ociserver.New(rmem, nil)
	agent := cueversion.UserAgent("cuelang.org/go")
	checked := false
	checkUserAgentHandler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		qt.Check(t, qt.Equals(req.UserAgent(), agent))
		checked = true
		rh.ServeHTTP(w, req)
	})
	srv := httptest.NewServer(checkUserAgentHandler)
	u, err := url.Parse(srv.URL)
	qt.Assert(t, qt.IsNil(err))

	dir := t.TempDir()
	t.Setenv("DOCKER_CONFIG", dir)
	t.Setenv("CUE_REGISTRY", u.Host+"+insecure")
	cacheDir := filepath.Join(dir, "cache")
	t.Setenv("CUE_CACHE_DIR", cacheDir)
	t.Cleanup(func() {
		modcache.RemoveAll(cacheDir)
	})

	r, err := NewRegistry(nil)
	qt.Assert(t, qt.IsNil(err))
	gotRequirements, err := r.Requirements(ctx, module.MustNewVersion("bar.example@v0", "v0.0.1"))
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.HasLen(gotRequirements, 0))

	qt.Assert(t, qt.IsTrue(checked))
}

// TestConcurrentTokenRefresh verifies that concurrent OAuth token refreshes,
// including logins.json updates, are properly synchronized.
func TestConcurrentTokenRefresh(t *testing.T) {
	// Start N registry instances, each containing one CUE module and running
	// in its own HTTP server instance. Each instance is protected with its
	// own OAuth token, which is initially expired, requiring a refresh token
	// request upon first invocation.
	var registries [20]struct {
		mod  string
		host string
	}
	var counter int32 = 0
	for i := range registries {
		reg := &registries[i]
		reg.mod = fmt.Sprintf("foo.mod%02d", i)
		fsys, err := txtar.FS(txtar.Parse([]byte(fmt.Sprintf(`
-- %s_v0.0.1/cue.mod/module.cue --
module: "%s@v0"
language: version: "v0.8.0"
-- %s_v0.0.1/bar/bar.cue --
package bar
`, reg.mod, reg.mod, reg.mod))))
		qt.Assert(t, qt.IsNil(err))
		mux := http.NewServeMux()
		r := ocimem.New()
		err = registrytest.Upload(context.Background(), r, fsys)
		qt.Assert(t, qt.IsNil(err))
		rh := ociserver.New(r, nil)
		mux.HandleFunc("/v2/", func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if !strings.HasPrefix(auth, fmt.Sprintf("Bearer access_%d_", i)) {
				w.WriteHeader(401)
				fmt.Fprintf(w, "server %d: unexpected auth header: %s", i, auth)
				return
			}
			rh.ServeHTTP(w, r)
		})
		mux.HandleFunc("/login/oauth/token", func(w http.ResponseWriter, r *http.Request) {
			ctr := atomic.AddInt32(&counter, 1)
			writeJSON(w, 200, oauth2.Token{
				AccessToken:  fmt.Sprintf("access_%d_%d", i, ctr),
				TokenType:    "Bearer",
				RefreshToken: fmt.Sprintf("refresh_%d", ctr),
				ExpiresIn:    300,
			})
		})
		srv := httptest.NewServer(mux)
		t.Cleanup(srv.Close)
		u, err := url.Parse(srv.URL)
		qt.Assert(t, qt.IsNil(err))
		reg.host = u.Host
	}

	expiry := time.Now()
	logins := &cueconfig.Logins{
		Registries: map[string]cueconfig.RegistryLogin{},
	}
	registryConf := ""
	for i, reg := range registries {
		logins.Registries[reg.host] = cueconfig.RegistryLogin{
			AccessToken:  fmt.Sprintf("access_%d_x", i),
			TokenType:    "Bearer",
			RefreshToken: "refresh_x",
			Expiry:       &expiry,
		}
		if registryConf != "" {
			registryConf += ","
		}
		registryConf += fmt.Sprintf("%s=%s+insecure", reg.mod, reg.host)
	}

	dir := t.TempDir()
	configDir := filepath.Join(dir, "config")
	t.Setenv("CUE_CONFIG_DIR", configDir)
	err := os.MkdirAll(configDir, 0o777)
	qt.Assert(t, qt.IsNil(err))

	// Check logins.json validation.
	logins.Registries["blank"] = cueconfig.RegistryLogin{TokenType: "Bearer"}
	err = cueconfig.WriteLogins(filepath.Join(configDir, "logins.json"), logins)
	delete(logins.Registries, "blank")
	qt.Assert(t, qt.IsNil(err))
	_, err = cueconfig.ReadLogins(filepath.Join(configDir, "logins.json"))
	qt.Assert(t, qt.ErrorMatches(err, "invalid .*logins.json: missing access_token for registry blank"))

	// Check write-read round-trip.
	err = cueconfig.WriteLogins(filepath.Join(configDir, "logins.json"), logins)
	qt.Assert(t, qt.IsNil(err))
	logins2, err := cueconfig.ReadLogins(filepath.Join(configDir, "logins.json"))
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.DeepEquals(logins2, logins))

	t.Setenv("CUE_REGISTRY", registryConf)
	cacheDir := filepath.Join(dir, "cache")
	t.Setenv("CUE_CACHE_DIR", cacheDir)
	t.Cleanup(func() {
		modcache.RemoveAll(cacheDir)
	})

	r, err := NewRegistry(nil)
	qt.Assert(t, qt.IsNil(err))

	g := new(errgroup.Group)
	for i := range registries {
		mod := registries[i].mod
		g.Go(func() error {
			ctx := context.Background()
			loc, err := r.Fetch(ctx, module.MustNewVersion(mod+"@v0", "v0.0.1"))
			if err != nil {
				return err
			}
			data, err := fs.ReadFile(loc.FS, path.Join(loc.Dir, "bar/bar.cue"))
			if err != nil {
				return err
			}
			if string(data) != "package bar\n" {
				return fmt.Errorf("unexpected data: %q", string(data))
			}
			return nil
		})
	}
	err = g.Wait()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(int(counter), len(registries)))
}

// dockerConfig describes the minimal subset of the docker
// configuration file necessary to check that authentication
// is correction hooked up.
type dockerConfig struct {
	Auths map[string]authConfig `json:"auths"`
}

// authConfig contains authorization information for connecting to a Registry.
type authConfig struct {
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func writeJSON(w http.ResponseWriter, statusCode int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		// should never happen
		panic(err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	w.Write(b)
}
