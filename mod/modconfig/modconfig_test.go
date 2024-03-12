package modconfig

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/internal/registrytest"
	"cuelang.org/go/internal/txtarfs"
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
deps: "bar.example@v0": v: "v0.0.1"
-- r1/foo.example_v0.0.1/bar/bar.cue --
package bar
-- r1/bar.example_v0.0.1/cue.mod/module.cue --
module: "bar.example@v0"
-- r1/bar.example_v0.0.1/y/y.cue --
package y

-- r2/auth.json --
{
	"username": "bob",
	"password": "somePassword"
}
-- r2/bar.example_v0.0.1/cue.mod/module.cue --
module: "bar.example@v0"
-- r2/bar.example_v0.0.1/x/x.cue --
package x
`))
	fsys := txtarfs.FS(modules)
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
