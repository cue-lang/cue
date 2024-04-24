package load_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/internal/cueexperiment"
	"cuelang.org/go/internal/cuetxtar"
	"cuelang.org/go/internal/registrytest"
	"cuelang.org/go/internal/txtarfs"
	"cuelang.org/go/mod/modcache"
)

func TestModuleLoadWithInvalidRegistryConfig(t *testing.T) {
	// When the modules experiment is enabled and there's an invalid
	// registry configuration, we shouldn't get an error unless the
	// module actually tries to use a registry.
	qt.Patch(t, &cueexperiment.Flags.Modules, true)
	t.Setenv("CUE_REGISTRY", "invalid}host:")
	cacheDir := t.TempDir()
	t.Setenv("CUE_CACHE_DIR", cacheDir)

	insts := load.Instances([]string{"./imports"}, &load.Config{
		Dir: filepath.Join("testdata", "testmod"),
	})
	qt.Assert(t, qt.IsNil(insts[0].Err))

	// Check that nothing has been created in the cache directory (no
	// side-effects).
	entries, err := os.ReadDir(cacheDir)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.HasLen(entries, 0))

	// Now check that we do get an error when we try to use a module
	// that requires module resolution.
	testData, err := os.ReadFile(filepath.Join("testdata", "testfetch", "simple.txtar"))
	qt.Assert(t, qt.IsNil(err))

	cfg := &load.Config{
		Dir:     t.TempDir(),
		Overlay: map[string]load.Source{},
	}
	a := txtar.Parse(testData)
	for _, f := range a.Files {
		if !strings.HasPrefix(f.Name, "_registry/") {
			cfg.Overlay[filepath.Join(cfg.Dir, f.Name)] = load.FromBytes(f.Data)
		}
	}
	insts = load.Instances([]string{"."}, cfg)
	qt.Assert(t, qt.ErrorMatches(insts[0].Err, `import failed: cannot find package "example.com@v0": cannot fetch example.com@v0.0.1: bad value for \$CUE_REGISTRY: invalid registry "invalid}host:": invalid host name "invalid}host:" in registry`))

	// Try again with environment variables passed in Env.
	// This is really just a smoke test to make sure that Env is
	// passed through to the underlying modconfig call.
	cfg.Env = []string{
		"CUE_REGISTRY=invalid}host2:",
		"CUE_CACHE_DIR=" + cacheDir,
	}
	insts = load.Instances([]string{"."}, cfg)
	qt.Assert(t, qt.ErrorMatches(insts[0].Err, `import failed: cannot find package "example.com@v0": cannot fetch example.com@v0.0.1: bad value for \$CUE_REGISTRY: invalid registry "invalid}host2:": invalid host name "invalid}host2:" in registry`))
}

func TestModuleFetch(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root: "./testdata/testfetch",
		Name: "modfetch",
	}
	test.Run(t, func(t *cuetxtar.Test) {
		rfs, err := fs.Sub(txtarfs.FS(t.Archive), "_registry")
		if err != nil {
			t.Fatal(err)
		}
		r, err := registrytest.New(rfs, "")
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()

		// We're testing that the default modconfig-based behavour works
		// as expected when the modules experiment is enabled.
		tmpDir := t.TempDir()
		t.LoadConfig.Env = []string{
			"CUE_CACHE_DIR=" + filepath.Join(tmpDir, "cache"),
			"CUE_REGISTRY=" + r.Host() + "+insecure",
			"CUE_CONFIG_DIR=" + filepath.Join(tmpDir, "config"),
		}
		// The fetched files are read-only, so testing fails when trying
		// to remove them.
		defer modcache.RemoveAll(tmpDir)
		qt.Patch(t, &cueexperiment.Flags.Modules, true)
		ctx := cuecontext.New()
		insts := t.RawInstances()
		if len(insts) != 1 {
			t.Fatalf("wrong instance count; got %d want 1", len(insts))
		}
		inst := insts[0]
		if inst.Err != nil {
			errors.Print(t.Writer("error"), inst.Err, &errors.Config{
				ToSlash: true,
				Cwd:     t.Dir,
			})
			return
		}
		v := ctx.BuildInstance(inst)
		if err := v.Validate(); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(t, "%v\n", v)
	})
}
