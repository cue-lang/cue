// Copyright 2026 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package runtime_test

import (
	"fmt"
	"reflect"
	goruntime "runtime"
	"strings"
	"sync"
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/stats"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/core/runtime"
)

type emptyScope struct{}

func (emptyScope) Parent() compile.Scope { return nil }
func (emptyScope) Vertex() *adt.Vertex   { return &adt.Vertex{} }

// newInstance returns an instance with src as its file.
func newInstance(t *testing.T, name, src string) *build.Instance {
	p := build.NewContext().NewInstance("", nil)
	//lint:ignore SA1019 AddFile is due to be renamed, not removed
	if err := p.AddFile(name+".cue", src); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestBuildConfigFields checks that every field of [runtime.Config], including
// those of the embedded [compile.Config], is classified as either changing
// what an instance builds to, so that [runtime.Runtime.Build] must not share
// such a build with other builds of the instance, or not.
func TestBuildConfigFields(t *testing.T) {
	r := runtime.New()
	// A non-zero value for each field which changes what an instance builds
	// to. A field added here must also be reported by Config.hasOptions.
	options := map[string]any{
		"Scope":      compile.Scope(emptyScope{}),
		"Imports":    func(*ast.Ident) string { return "" },
		"ImportPath": "mod.test/other",
	}
	// A non-zero value for each field which does not, or nil for a field
	// which cannot be set here.
	neutral := map[string]any{
		"Runtime":  r,
		"Filename": "other.cue",
		"Counts":   &stats.Counts{},
		"Config":   nil, // the embedded compile.Config, whose fields are listed
		"pkgPath":  nil, // set by the compiler from the instance's ID
	}

	for _, f := range reflect.VisibleFields(reflect.TypeFor[runtime.Config]()) {
		val, isOption := options[f.Name]
		if !isOption {
			var ok bool
			if val, ok = neutral[f.Name]; !ok {
				t.Errorf("runtime.Config field %s is not classified as changing what an instance builds to or not", f.Name)
				continue
			}
		}
		if val == nil {
			continue
		}
		t.Run(f.Name, func(t *testing.T) {
			var cfg runtime.Config
			reflect.ValueOf(&cfg).Elem().FieldByIndex(f.Index).Set(reflect.ValueOf(val))

			b := newInstance(t, "a", "a: 1")
			plain, err := r.Build(nil, b)
			if err != nil {
				t.Fatal(err)
			}
			withField, err := r.Build(&cfg, b)
			if err != nil {
				t.Fatal(err)
			}
			if shared := withField == plain; shared == isOption {
				t.Errorf("build with %s shared with a plain build: %v, want %v", f.Name, shared, !isOption)
			}
			if again, _ := r.Build(nil, b); again != plain {
				t.Errorf("plain build after a build with %s is not shared with the first", f.Name)
			}
		})
	}
}

// TestInstanceFromNode tests that a root only leads to its build instance in
// the runtime which built it, whether or not that build is shared.
func TestInstanceFromNode(t *testing.T) {
	r, other := runtime.New(), runtime.New()
	built, err := r.Build(nil, newInstance(t, "a", "a: 1"))
	if err != nil {
		t.Fatal(err)
	}
	compiled, _ := r.Compile(nil, "a: 1")
	for _, root := range []*adt.Vertex{built, compiled} {
		if r.GetInstanceFromNode(root) == nil {
			t.Error("root does not lead to its instance")
		}
		if other.GetInstanceFromNode(root) != nil {
			t.Error("root leads to an instance in another runtime")
		}
	}
}

// barrier is an injection which holds each build that it is part of until
// all the builds which it waits for have compiled their instance.
type barrier struct{ builds sync.WaitGroup }

func (b *barrier) Kind() string { return "barrier" }

func (b *barrier) InjectorForInstance(*build.Instance, *runtime.Runtime) (runtime.Injector, errors.Error) {
	b.builds.Done()
	b.builds.Wait()
	return nil, nil // there is nothing to inject
}

// TestBuildOverlapping tests that overlapping builds of an instance can each
// evaluate their root in place at the same time, as Context.BuildInstance
// does, so they must not share one.
func TestBuildOverlapping(t *testing.T) {
	var src strings.Builder
	src.WriteString("@extern(barrier)\n\npackage p\n\n")
	for i := range 30 {
		fmt.Fprintf(&src, "#D%d: {a: int, b: string | *\"x\"}\nf%d: #D%d & {a: %d}\n", i, i, i, i)
	}
	r := runtime.New()
	var bar barrier
	bar.builds.Add(2)
	r.AddInjection(&bar)
	p := newInstance(t, "p", src.String())
	if err := p.Complete(); err != nil {
		t.Fatal(err)
	}

	var roots [2]*adt.Vertex
	var errs [2]errors.Error
	var wg sync.WaitGroup
	for i := range roots {
		wg.Go(func() { roots[i], errs[i] = r.Build(nil, p) })
	}
	wg.Wait()
	if roots[0] == roots[1] {
		t.Fatal("overlapping builds share the root which each evaluates in place")
	}
	for i, root := range roots {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		wg.Go(func() {
			ctx := adt.NewContext(r, root)
			root.Finalize(ctx)
			if err := root.Err(ctx); err != nil {
				t.Error(err.Err)
			}
		})
	}
	wg.Wait()
}

// handBuilt returns an instance of package name with src as its body, whose
// import path is "x.test/" followed by name.
func handBuilt(t *testing.T, name, src string) *build.Instance {
	p := newInstance(t, name, "package "+name+"\n"+src)
	p.ImportPath, p.PkgName = "x.test/"+name, name
	return p
}

// TestBuildOverlappingImporters tests that an import still resolves in its
// own runtime once only one of two overlapping builds which import the same
// package is referenced, whichever of their roots for it was recorded last.
func TestBuildOverlappingImporters(t *testing.T) {
	type overlap struct {
		r    *runtime.Runtime
		kept *adt.Vertex
	}
	var overlaps []overlap
	// Which build records its root last varies, so repeat.
	for range 20 {
		r := runtime.New()
		var bar barrier
		bar.builds.Add(2)
		r.AddInjection(&bar)
		s := newInstance(t, "s", "@extern(barrier)\n\npackage s\n\nX: 1\n")
		s.ImportPath = "x.test/s"
		var importers [2]*build.Instance
		for i := range importers {
			p := handBuilt(t, "p", `import "x.test/s"`+"\nY: s.X\n")
			p.Imports = []*build.Instance{s}
			importers[i] = p
		}
		if err := s.Complete(); err != nil {
			t.Fatal(err)
		}
		var kept *adt.Vertex
		var wg sync.WaitGroup
		wg.Go(func() { kept, _ = r.Build(nil, importers[0]) })
		wg.Go(func() { r.Build(nil, importers[1]) })
		wg.Wait()
		overlaps = append(overlaps, overlap{r, kept})
	}
	goruntime.GC()
	for _, o := range overlaps {
		ctx := adt.NewContext(o.r, o.kept)
		o.kept.Finalize(ctx)
		if err := o.kept.Err(ctx); err != nil {
			t.Fatalf("import does not resolve after a collection: %v", err.Err)
		}
	}
}

// TestBuildUnreferencedImports tests that building an instance builds none
// of the packages in its Imports which it does not use.
func TestBuildUnreferencedImports(t *testing.T) {
	t.Run("NoFiles", func(t *testing.T) {
		a, b := &build.Instance{}, &build.Instance{}
		a.Imports = []*build.Instance{b}
		b.Imports = []*build.Instance{a}
		if _, err := runtime.New().Build(nil, a); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("SameList", func(t *testing.T) {
		// Each instance lists all the packages, itself included. The package
		// c lists b, which imports a, which imports c, but c uses neither.
		a := handBuilt(t, "a", `import "x.test/c"`+"\nA: c.C\n")
		b := handBuilt(t, "b", `import "x.test/a"`+"\nB: a.A\n")
		c := handBuilt(t, "c", "C: 1\n")
		all := []*build.Instance{a, b, c}
		for _, p := range all {
			p.Imports = all
		}
		r := runtime.New()
		for _, p := range all {
			if _, err := r.Build(nil, p); err != nil {
				t.Errorf("%s: %v", p.ImportPath, err)
			}
		}
	})
}
