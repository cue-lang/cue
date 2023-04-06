package load_test

import (
	"fmt"
	"sort"
	"testing"

	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/cue/load/internal/registrytest"
	"cuelang.org/go/internal/cuetxtar"
)

func TestModuleFetch(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root: "./testdata/testfetch",
		Name: "modfetch",
	}
	test.Run(t, func(t *cuetxtar.Test) {
		r := registrytest.New(t.Archive)
		defer r.Close()
		t.LoadConfig.Registry = r.URL()
		ctx := cuecontext.New()
		insts := t.RawInstances()
		if len(insts) != 1 {
			t.Fatalf("wrong instance count; got %d want 1", len(insts))
		}
		inst := insts[0]
		badImps := make(map[string]bool)
		addNonExistentImports(inst, badImps)
		if len(badImps) > 0 {
			w := t.Writer("nonexistent-imports")
			for _, pkg := range sortedMapKeys(badImps) {
				fmt.Fprintln(w, pkg)
			}
		}
		if inst.Err != nil {
			errors.Print(t.Writer("error"), inst.Err, &errors.Config{
				Cwd:     t.Dir,
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

func addNonExistentImports(inst *build.Instance, m map[string]bool) {
	var err *load.PackageError
	if inst.ImportPath != "" && errors.As(inst.Err, &err) && err.IsNotExist {
		m[inst.ImportPath] = true
	}
	for _, inst := range inst.Imports {
		addNonExistentImports(inst, m)
	}
}

func sortedMapKeys[V any](m map[string]V) []string {
	s := make([]string, 0, len(m))
	for k := range m {
		s = append(s, k)
	}
	sort.Strings(s)
	return s
}
