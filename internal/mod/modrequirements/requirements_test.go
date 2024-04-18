package modrequirements

import (
	"context"
	"fmt"
	"testing"

	"cuelabs.dev/go/oci/ociregistry/ociclient"
	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/internal/mod/mvs"
	"cuelang.org/go/internal/registrytest"
	"cuelang.org/go/internal/txtarfs"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/modregistry"
	"cuelang.org/go/mod/module"
)

func TestRequirements(t *testing.T) {
	const registryContents = `
-- example.com_v0.0.1/cue.mod/module.cue --
module: "example.com@v0"
language: version: "v0.8.0"
deps: {
	"foo.com/bar/hello@v0": v: "v0.2.3"
	"bar.com@v0": v: "v0.5.0"
}

-- foo.com_bar_hello_v0.2.3/cue.mod/module.cue --
module: "foo.com/bar/hello@v0"
language: version: "v0.8.0"
deps: {
	"bar.com@v0": v: "v0.0.2"
	"baz.org@v0": v: "v0.10.1"
}

-- bar.com_v0.0.2/cue.mod/module.cue --
module: "bar.com@v0"
language: version: "v0.8.0"
deps: "baz.org@v0": v: "v0.0.2"

-- bar.com_v0.5.0/cue.mod/module.cue --
module: "bar.com@v0"
language: version: "v0.8.0"
deps: "baz.org@v0": v: "v0.5.0"

-- baz.org_v0.0.2/cue.mod/module.cue --
module: "baz.org@v0"
language: version: "v0.8.0"

-- baz.org_v0.1.2/cue.mod/module.cue --
module: "baz.org@v0"
language: version: "v0.8.0"

-- baz.org_v0.5.0/cue.mod/module.cue --
module: "baz.org@v0"
language: version: "v0.8.0"

-- baz.org_v0.10.1/cue.mod/module.cue --
module: "baz.org@v0"
language: version: "v0.8.0"
`

	ctx := context.Background()
	reg := newRegistry(t, registryContents)

	rootVersion := mustParseVersion("example.com@v0")

	rs := NewRequirements(rootVersion.Path(), reg, versions("foo.com/bar/hello@v0.2.3"), nil)

	v, ok := rs.RootSelected(rootVersion.Path())
	qt.Assert(t, qt.IsTrue(ok))
	qt.Assert(t, qt.Equals(v, ""))

	v, ok = rs.RootSelected("foo.com/bar/hello@v0")
	qt.Assert(t, qt.IsTrue(ok))
	qt.Assert(t, qt.Equals(v, "v0.2.3"))

	// Other parts of the graph aren't loaded yet.
	v, ok = rs.RootSelected("bar.com@v0")
	qt.Assert(t, qt.IsFalse(ok))
	qt.Assert(t, qt.Equals(v, ""))

	mg, err := rs.Graph(ctx)
	qt.Assert(t, qt.IsNil(err))
	_ = mg
	rv, ok := mg.RequiredBy(rootVersion)
	qt.Assert(t, qt.Equals(ok, true))
	qt.Assert(t, qt.DeepEquals(rv, []module.Version{
		module.MustParseVersion("foo.com/bar/hello@v0.2.3"),
	}))
	rv, ok = mg.RequiredBy(module.MustParseVersion("foo.com/bar/hello@v0.2.3"))
	qt.Assert(t, qt.Equals(ok, true))
	qt.Assert(t, qt.DeepEquals(rv, versions("bar.com@v0.0.2", "baz.org@v0.10.1")))

	qt.Assert(t, qt.DeepEquals(mg.BuildList(), versions(
		"example.com@v0",
		"bar.com@v0.0.2",
		"baz.org@v0.10.1",
		"foo.com/bar/hello@v0.2.3",
	)))
}

func TestRequirementsErrorFromMissingModule(t *testing.T) {
	const registryContents = `
-- example.com_v0.0.1/cue.mod/module.cue --
module: "example.com@v0"
language: version: "v0.8.0"
deps: "foo.com/bar/hello@v0": v: "v0.2.3"

-- foo.com_bar_hello_v0.2.3/cue.mod/module.cue --
module: "foo.com/bar/hello@v0"
language: version: "v0.8.0"
deps: "bar.com@v0": v: "v0.0.2"	// doesn't exist
`
	ctx := context.Background()
	reg := newRegistry(t, registryContents)

	rootVersion := mustParseVersion("example.com@v0")
	rs := NewRequirements(rootVersion.Path(), reg, versions(
		"bar.com@v0.0.2",
		"foo.com/bar/hello@v0.2.3",
	), nil)
	_, err := rs.Graph(ctx)
	qt.Assert(t, qt.ErrorMatches(err, `bar.com@v0.0.2: module bar.com@v0.0.2: 404 Not Found: name unknown: repository name not known to registry`))
	qt.Assert(t, qt.ErrorAs(err, new(*mvs.BuildListError[module.Version])))
}

func TestRequirementsWithDefaultMajorVersions(t *testing.T) {
	rs := NewRequirements("example.com@v0", nil, versions(
		"bar.com@v0.0.2",
		"bar.com@v1.2.3",
		"bar.com@v2.0.1",
		"baz.org@v0.10.1",
		"baz.org@v1.2.3",
		"foo.com/bar/hello@v0.2.3",
	), map[string]string{
		"bar.com": "v1",
	})
	qt.Assert(t, qt.DeepEquals(rs.DefaultMajorVersions(), map[string]string{
		"bar.com": "v1",
	}))
	tests := []struct {
		mpath       string
		wantVersion string
		wantStatus  MajorVersionDefaultStatus
	}{{
		mpath:       "bar.com",
		wantVersion: "v1",
		wantStatus:  ExplicitDefault,
	}, {
		mpath:      "baz.org",
		wantStatus: AmbiguousDefault,
	}, {
		mpath:       "foo.com/bar/hello",
		wantVersion: "v0",
		wantStatus:  NonExplicitDefault,
	}, {
		mpath:       "other.com",
		wantVersion: "",
		wantStatus:  NoDefault,
	}}
	for _, test := range tests {
		t.Run("", func(t *testing.T) {
			v, st := rs.DefaultMajorVersion(test.mpath)
			qt.Check(t, qt.Equals(v, test.wantVersion))
			qt.Check(t, qt.Equals(st, test.wantStatus))
		})
	}

}

type registryImpl struct {
	reg *modregistry.Client
}

var _ Registry = (*registryImpl)(nil)

func (r *registryImpl) Requirements(ctx context.Context, mv module.Version) ([]module.Version, error) {
	m, err := r.reg.GetModule(ctx, mv)
	if err != nil {
		return nil, err
	}
	data, err := m.ModuleFile(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot get module file from %v: %v", m, err)
	}
	mf, err := modfile.Parse(data, mv.String())
	if err != nil {
		return nil, fmt.Errorf("cannot parse module file from %v: %v", m, err)
	}
	return mf.DepVersions(), nil
}

func versions(vs ...string) []module.Version {
	mvs := make([]module.Version, len(vs))
	for i, v := range vs {
		mvs[i] = mustParseVersion(v)
	}
	return mvs
}

// mustParseVersion is like module.MustParseVersion except
// that it accepts non-versioned modules too, e.g. foo.com@v0.
func mustParseVersion(s string) module.Version {
	if v, err := module.ParseVersion(s); err == nil {
		return v
	}
	v, err := module.NewVersion(s, "")
	if err != nil {
		panic(err)
	}
	return v
}

func newRegistry(t *testing.T, registryContents string) Registry {
	regSrv, err := registrytest.New(txtarfs.FS(txtar.Parse([]byte(registryContents))), "")
	qt.Assert(t, qt.IsNil(err))
	t.Cleanup(regSrv.Close)
	regOCI, err := ociclient.New(regSrv.Host(), &ociclient.Options{
		Insecure: true,
	})
	qt.Assert(t, qt.IsNil(err))
	return &registryImpl{modregistry.NewClient(regOCI)}
}
