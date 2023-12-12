package load_test

import (
	"fmt"
	"io/fs"
	"testing"

	"cuelabs.dev/go/oci/ociregistry/ociclient"

	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal/cuetxtar"
	"cuelang.org/go/internal/mod/modcache"
	"cuelang.org/go/internal/registrytest"
	"cuelang.org/go/internal/txtarfs"
)

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
		reg, err := ociclient.New(r.Host(), &ociclient.Options{
			Insecure: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		cacheDir := t.TempDir()
		// The fetched files are read-only, so testing fails when trying
		// to remove them.
		defer modcache.RemoveAll(cacheDir)
		reg1, err := modcache.New(reg, cacheDir)
		if err != nil {
			t.Fatal(err)
		}
		t.LoadConfig.Registry = reg1
		ctx := cuecontext.New()
		insts := t.RawInstances()
		if len(insts) != 1 {
			t.Fatalf("wrong instance count; got %d want 1", len(insts))
		}
		inst := insts[0]
		if inst.Err != nil {
			errors.Print(t.Writer("error"), inst.Err, &errors.Config{
				ToSlash: true,
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
