// Copyright 2026 The CUE Authors
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

package cueload_test

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/stats"
	cue "cuelang.org/go/cue/v2"
	"cuelang.org/go/cueload"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/module"
)

var ctxbg = context.Background()

const moduleCUE = `module: "main.example@v0"
language: version: "v0.9.0"
`

func baseFiles() map[string]string {
	return map[string]string{
		"work/cue.mod/module.cue": moduleCUE,
		"work/util/util.cue": `package util

greet: "hello"
`,
		"work/hello/hello.cue": `package hello

import "main.example/util"

greeting: util.greet + ", world"
`,
		"work/hello/config.yaml": "x: 1\n",
		"work/bar/bar.cue": `package bar

x: 1
`,
		"work/data.yaml": "a: 1\n---\na: hello\n---\nb: 2\n",
	}
}

func testFS(files map[string]string) fstest.MapFS {
	m := make(fstest.MapFS)
	for name, content := range files {
		m[name] = &fstest.MapFile{Data: []byte(content)}
	}
	return m
}

func newLoader(t *testing.T, files map[string]string, mut func(*cueload.Config)) *cueload.Loader {
	t.Helper()
	cfg := &cueload.Config{
		FS:  testFS(files),
		Dir: "/work",
	}
	if mut != nil {
		mut(cfg)
	}
	l, err := cueload.New(cfg)
	qt.Assert(t, qt.IsNil(err))
	return l
}

func lookupString(t *testing.T, v cue.Value, p string) string {
	t.Helper()
	s, err := v.LookupPath(cue.ParsePath(p)).AsString(ctxbg)
	qt.Assert(t, qt.IsNil(err))
	return s
}

func lookupInt(t *testing.T, v cue.Value, p string) int64 {
	t.Helper()
	n, err := v.LookupPath(cue.ParsePath(p)).Int64(ctxbg)
	qt.Assert(t, qt.IsNil(err))
	return n
}

func TestPackagesCanonicalization(t *testing.T) {
	// The #4264 property: all spellings of an import path yield the
	// same canonical *Package.
	l := newLoader(t, baseFiles(), nil)
	spellings := []string{
		"main.example/bar",
		"main.example/bar@v0",
		"main.example/bar@v0:bar",
		"./bar",
	}
	var pkgs []*cueload.Package
	for _, s := range spellings {
		pkg, err := l.Package(ctxbg, s)
		qt.Assert(t, qt.IsNil(err), qt.Commentf("pattern %q", s))
		pkgs = append(pkgs, pkg)
	}
	for i, pkg := range pkgs[1:] {
		qt.Assert(t, qt.Equals(pkg, pkgs[0]), qt.Commentf("pattern %q", spellings[i+1]))
	}
	qt.Assert(t, qt.Equals(pkgs[0].ImportPath().String(), "main.example/bar@v0:bar"))
	qt.Assert(t, qt.Equals(pkgs[0].Name(), "bar"))
}

func TestPackageValueAndImports(t *testing.T) {
	l := newLoader(t, baseFiles(), nil)
	pkg, err := l.Package(ctxbg, "./hello")
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsNil(pkg.Err()))
	qt.Assert(t, qt.Equals(pkg.Name(), "hello"))
	qt.Assert(t, qt.Equals(pkg.Dir(), "/work/hello"))
	qt.Assert(t, qt.Equals(pkg.Module().Path(), "main.example@v0"))

	files := pkg.Files()
	qt.Assert(t, qt.HasLen(files, 1))
	qt.Assert(t, qt.Equals(files[0].Name, "/work/hello/hello.cue"))

	data := pkg.DataFiles()
	qt.Assert(t, qt.HasLen(data, 1))
	qt.Assert(t, qt.Equals(data[0].Name, "/work/hello/config.yaml"))

	v, err := pkg.Value(ctxbg)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(lookupString(t, v, "greeting"), "hello, world"))

	imports := pkg.Imports()
	qt.Assert(t, qt.HasLen(imports, 1))
	qt.Assert(t, qt.Equals(imports[0].Name(), "util"))

	// The same package arrived at through the import graph is the
	// canonical one.
	util, err := l.Package(ctxbg, "./util")
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(imports[0], util))

	// PackageOf identifies the package of the root value and of values
	// reached from it.
	got, ok := l.PackageOf(v)
	qt.Assert(t, qt.IsTrue(ok))
	qt.Assert(t, qt.Equals(got, pkg))

	field := v.LookupPath(cue.ParsePath("greeting"))
	qt.Assert(t, qt.IsNil(field.Err(ctxbg))) // force
	got, ok = l.PackageOf(field)
	qt.Assert(t, qt.IsTrue(ok))
	qt.Assert(t, qt.Equals(got, pkg))
}

func TestStdlibImport(t *testing.T) {
	files := baseFiles()
	files["work/su/su.cue"] = `package su

import "strings"

up: strings.ToUpper("abc")
`
	l := newLoader(t, files, nil)
	pkg, err := l.Package(ctxbg, "./su")
	qt.Assert(t, qt.IsNil(err))
	v, err := pkg.Value(ctxbg)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(lookupString(t, v, "up"), "ABC"))

	imports := pkg.Imports()
	qt.Assert(t, qt.HasLen(imports, 1))
	qt.Assert(t, qt.Equals(imports[0].ImportPath().String(), "strings:strings"))
	qt.Assert(t, qt.IsNil(imports[0].Module()))
}

// valueResolver provides two virtual packages: one built via
// NewValuePackage from a value, one via NewPackage from CUE files.
type valueResolver struct {
	l *cueload.Loader
}

func (r *valueResolver) ResolvePackage(ctx context.Context, ip ast.ImportPath) (*cueload.Package, error) {
	switch ip.Path {
	case "virtual.example/hi":
		f, err := parser.ParseFile("hi.cue", "package hi\n\ngreeting: \"virtual hello\"\n")
		if err != nil {
			return nil, err
		}
		v, err := r.l.Build(ctx, f)
		if err != nil {
			return nil, err
		}
		return r.l.NewValuePackage(ip, v)
	case "virtual.example/vpkg":
		return r.l.NewPackage(ctx, ip, cueload.File{
			Name: "vpkg.cue",
			Data: []byte("package vpkg\n\nimport \"strings\"\n\nup: strings.ToUpper(\"virtual\")\n"),
		})
	}
	return nil, nil
}

func TestResolveHook(t *testing.T) {
	files := baseFiles()
	files["work/vmain/vmain.cue"] = `package vmain

import (
	"virtual.example/hi"
	"virtual.example/vpkg"
)

a: hi.greeting
b: vpkg.up
`
	resolver := &valueResolver{}
	l := newLoader(t, files, func(cfg *cueload.Config) {
		cfg.Resolve = resolver
	})
	resolver.l = l

	pkg, err := l.Package(ctxbg, "./vmain")
	qt.Assert(t, qt.IsNil(err))
	v, err := pkg.Value(ctxbg)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsNil(v.Validate(ctxbg)))
	qt.Assert(t, qt.Equals(lookupString(t, v, "a"), "virtual hello"))
	qt.Assert(t, qt.Equals(lookupString(t, v, "b"), "VIRTUAL"))

	// The virtual packages appear in the import list, without a module.
	var names []string
	for _, imp := range pkg.Imports() {
		names = append(names, imp.ImportPath().String())
		qt.Assert(t, qt.IsNil(imp.Module()))
	}
	slices.Sort(names)
	qt.Assert(t, qt.DeepEquals(names, []string{"virtual.example/hi:hi", "virtual.example/vpkg:vpkg"}))
}

func TestDecodeYAMLStream(t *testing.T) {
	l := newLoader(t, baseFiles(), nil)
	f := cueload.File{Name: "data.yaml"}

	var docs []cueload.Doc
	for doc, err := range l.Decode(ctxbg, f) {
		qt.Assert(t, qt.IsNil(err))
		docs = append(docs, doc)
	}
	qt.Assert(t, qt.HasLen(docs, 3))
	qt.Assert(t, qt.Equals(docs[1].Index, 1))

	v, err := docs[0].Value(ctxbg)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(lookupInt(t, v, "a"), int64(1)))

	// The algebra view yields the same three documents, with origins.
	var vals []cue.Value
	for v, err := range l.Load(ctxbg, cueload.Decode(f)) {
		qt.Assert(t, qt.IsNil(err))
		vals = append(vals, v)
	}
	qt.Assert(t, qt.HasLen(vals, 3))
	qt.Assert(t, qt.Equals(lookupString(t, vals[1], "a"), "hello"))

	o, ok := l.OriginOf(vals[2])
	qt.Assert(t, qt.IsTrue(ok))
	qt.Assert(t, qt.Equals(o.File.Name, "data.yaml"))
	qt.Assert(t, qt.Equals(o.Index, 2))
	qt.Assert(t, qt.IsNil(o.Package))

	// Origins from Doc.Value are recorded too.
	o, ok = l.OriginOf(v)
	qt.Assert(t, qt.IsTrue(ok))
	qt.Assert(t, qt.Equals(o.Index, 0))
}

func TestOriginOfPackageValue(t *testing.T) {
	l := newLoader(t, baseFiles(), nil)
	var vals []cue.Value
	for v, err := range l.Load(ctxbg, cueload.Pkg("./bar")) {
		qt.Assert(t, qt.IsNil(err))
		vals = append(vals, v)
	}
	qt.Assert(t, qt.HasLen(vals, 1))
	o, ok := l.OriginOf(vals[0])
	qt.Assert(t, qt.IsTrue(ok))
	qt.Assert(t, qt.IsNotNil(o.Package))
	qt.Assert(t, qt.Equals(o.Package.Name(), "bar"))
}

func TestLoadPkgFiles(t *testing.T) {
	l := newLoader(t, baseFiles(), nil)
	src := cueload.PkgFiles(
		cueload.File{Name: "a.cue", Data: []byte("package x\n\na: 1\n")},
		cueload.File{Name: "b.cue", Data: []byte("package x\n\nimport \"strings\"\n\nb: strings.ToUpper(c)\nc: \"go\"\n")},
	)
	v, err := l.LoadValue(ctxbg, src)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(lookupInt(t, v, "a"), int64(1)))
	qt.Assert(t, qt.Equals(lookupString(t, v, "b"), "GO"))
}

func TestGoAndSyntaxSources(t *testing.T) {
	l := newLoader(t, baseFiles(), nil)

	v, err := l.LoadValue(ctxbg, cueload.Go(map[string]int{"a": 1}))
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(lookupInt(t, v, "a"), int64(1)))

	f, err := parser.ParseFile("s.cue", "c: 3\n")
	qt.Assert(t, qt.IsNil(err))
	v, err = l.LoadValue(ctxbg, cueload.Syntax(f))
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(lookupInt(t, v, "c"), int64(3)))

	// Value round-trips through the algebra.
	v2, err := l.LoadValue(ctxbg, cueload.Value(v))
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(v2, v))
}

func TestUnifyBroadcastValidate(t *testing.T) {
	// A schema broadcast over a multi-document stream via Validate:
	// bad documents yield per-item errors while good documents still
	// flow.
	l := newLoader(t, baseFiles(), nil)
	schemaFile, err := parser.ParseFile("schema.cue", "a: int\n")
	qt.Assert(t, qt.IsNil(err))

	src := cueload.Validate(
		cueload.Decode(cueload.File{Name: "data.yaml"}),
		cueload.Syntax(schemaFile),
		cueload.Concrete(true),
	)
	type result struct {
		A   int64
		Err bool
	}
	var results []result
	for v, err := range l.Load(ctxbg, src) {
		if err != nil {
			results = append(results, result{Err: true})
			continue
		}
		results = append(results, result{A: lookupInt(t, v, "a")})
	}
	qt.Assert(t, qt.DeepEquals(results, []result{
		{A: 1},      // a: 1 is valid
		{Err: true}, // a: "hello" conflicts with a: int
		{Err: true}, // b: 2 leaves a non-concrete
	}))
}

func TestUnifyBroadcast(t *testing.T) {
	l := newLoader(t, baseFiles(), nil)
	// Broadcast a singular Go value over the document stream.
	src := cueload.Unify(
		cueload.Decode(cueload.File{Name: "data.yaml"}),
		cueload.Go(map[string]int{"extra": 7}),
	)
	var count int
	for v, err := range l.Load(ctxbg, src) {
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(lookupInt(t, v, "extra"), int64(7)))
		// Origins are preserved from the plural operand.
		o, ok := l.OriginOf(v)
		qt.Assert(t, qt.IsTrue(ok))
		qt.Assert(t, qt.Equals(o.Index, count))
		count++
	}
	qt.Assert(t, qt.Equals(count, 3))

	// Unifying all-singular operands yields a single value.
	v, err := l.LoadValue(ctxbg, cueload.Unify(cueload.Go(map[string]int{"a": 1}), cueload.Go(map[string]int{"b": 2})))
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(lookupInt(t, v, "a"), int64(1)))
	qt.Assert(t, qt.Equals(lookupInt(t, v, "b"), int64(2)))
}

func TestUnifyCardinalityError(t *testing.T) {
	l := newLoader(t, baseFiles(), nil)
	f := cueload.File{Name: "data.yaml"}
	src := cueload.Unify(cueload.Decode(f), cueload.Decode(f))
	_, err := l.LoadValue(ctxbg, src)
	qt.Assert(t, qt.ErrorMatches(err, ".*more than one plural operand.*"))
}

func TestAtLookup(t *testing.T) {
	l := newLoader(t, baseFiles(), nil)

	v, err := l.LoadValue(ctxbg, cueload.At(cueload.Go(1), cue.ParsePath("a.b")))
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(lookupInt(t, v, "a.b"), int64(1)))

	v, err = l.LoadValue(ctxbg, cueload.Lookup(cueload.Go(map[string]map[string]int{"a": {"b": 2}}), cue.ParsePath("a.b")))
	qt.Assert(t, qt.IsNil(err))
	n, err := v.Int64(ctxbg)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(n, int64(2)))
}

func TestAsListConcat(t *testing.T) {
	l := newLoader(t, baseFiles(), nil)

	v, err := l.LoadValue(ctxbg, cueload.AsList(cueload.Decode(cueload.File{Name: "data.yaml"})))
	qt.Assert(t, qt.IsNil(err))
	var elems []cue.Value
	for _, e := range v.Items(ctxbg) {
		elems = append(elems, e)
	}
	qt.Assert(t, qt.HasLen(elems, 3))
	qt.Assert(t, qt.Equals(lookupInt(t, elems[0], "a"), int64(1)))
	qt.Assert(t, qt.Equals(lookupString(t, elems[1], "a"), "hello"))

	var got []int64
	for v, err := range l.Load(ctxbg, cueload.Concat(cueload.Go(1), cueload.Go(2))) {
		qt.Assert(t, qt.IsNil(err))
		n, err := v.Int64(ctxbg)
		qt.Assert(t, qt.IsNil(err))
		got = append(got, n)
	}
	qt.Assert(t, qt.DeepEquals(got, []int64{1, 2}))
}

func TestEvalCombinator(t *testing.T) {
	l := newLoader(t, baseFiles(), nil)
	expr, err := parser.ParseExpr("expr", "a+b")
	qt.Assert(t, qt.IsNil(err))
	src := cueload.Eval(cueload.Go(map[string]int{"a": 1, "b": 2}), expr)
	v, err := l.LoadValue(ctxbg, src)
	qt.Assert(t, qt.IsNil(err))
	n, err := v.Int64(ctxbg)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(n, int64(3)))
}

func TestMapCombinator(t *testing.T) {
	l := newLoader(t, baseFiles(), nil)
	src := cueload.Map(
		cueload.Concat(cueload.Go(1), cueload.Go(2)),
		func(ctx context.Context, v cue.Value) (cue.Value, error) {
			n, err := v.Int64(ctx)
			if err != nil {
				return cue.Value{}, err
			}
			if n == 2 {
				return cue.Value{}, fmt.Errorf("two is not allowed")
			}
			return v, nil
		},
	)
	type result struct {
		N   int64
		Err string
	}
	var results []result
	for v, err := range l.Load(ctxbg, src) {
		if err != nil {
			results = append(results, result{Err: err.Error()})
			continue
		}
		n, err := v.Int64(ctxbg)
		qt.Assert(t, qt.IsNil(err))
		results = append(results, result{N: n})
	}
	qt.Assert(t, qt.DeepEquals(results, []result{
		{N: 1},
		{Err: "two is not allowed"},
	}))
}

func TestLoadValueArity(t *testing.T) {
	l := newLoader(t, baseFiles(), nil)
	_, err := l.LoadValue(ctxbg, cueload.Decode(cueload.File{Name: "data.yaml"}))
	qt.Assert(t, qt.ErrorMatches(err, ".*expected exactly one value, got 3.*"))
}

func TestBuildTags(t *testing.T) {
	files := baseFiles()
	files["work/tagged/base.cue"] = "package tagged\n\nbase: true\n"
	files["work/tagged/prod.cue"] = "@if(prod)\n\npackage tagged\n\nenv: \"prod\"\n"

	// Without the build tag the @if(prod) file is excluded.
	l := newLoader(t, files, nil)
	pkg, err := l.Package(ctxbg, "./tagged")
	qt.Assert(t, qt.IsNil(err))
	v, err := pkg.Value(ctxbg)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsFalse(v.LookupPath(cue.ParsePath("env")).Exists(ctxbg)))

	// With the build tag it is included.
	l = newLoader(t, files, func(cfg *cueload.Config) {
		cfg.BuildTags = []string{"prod"}
	})
	pkg, err = l.Package(ctxbg, "./tagged")
	qt.Assert(t, qt.IsNil(err))
	v, err = pkg.Value(ctxbg)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(lookupString(t, v, "env"), "prod"))
}

func TestTagInjection(t *testing.T) {
	files := baseFiles()
	files["work/tags/tags.cue"] = `package tags

env:  string @tag(env)
num:  int    @tag(num,type=int)
mode: string @tag(mode,short=fast|slow)
host: string @tag(host,var=myhost)
`
	l := newLoader(t, files, func(cfg *cueload.Config) {
		cfg.Tags = map[string]string{
			"env":  "prod",
			"num":  "42",
			"fast": "",
		}
		cfg.TagVars = map[string]cueload.TagVar{
			"myhost": {
				Func: func() (ast.Expr, error) {
					return ast.NewString("h1"), nil
				},
			},
		}
	})
	pkg, err := l.Package(ctxbg, "./tags")
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsNil(pkg.Err()))
	v, err := pkg.Value(ctxbg)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(lookupString(t, v, "env"), "prod"))
	qt.Assert(t, qt.Equals(lookupInt(t, v, "num"), int64(42)))
	qt.Assert(t, qt.Equals(lookupString(t, v, "mode"), "fast"))
	qt.Assert(t, qt.Equals(lookupString(t, v, "host"), "h1"))
}

func TestIncludeTests(t *testing.T) {
	files := baseFiles()
	files["work/t/a.cue"] = "package t\n\nx: 1\n"
	files["work/t/a_test.cue"] = "package t\n\ny: 2\n"

	l := newLoader(t, files, nil)
	pkg, err := l.Package(ctxbg, "./t")
	qt.Assert(t, qt.IsNil(err))
	v, err := pkg.Value(ctxbg)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsFalse(v.LookupPath(cue.ParsePath("y")).Exists(ctxbg)))

	l = newLoader(t, files, func(cfg *cueload.Config) {
		cfg.IncludeTests = true
	})
	pkg, err = l.Package(ctxbg, "./t")
	qt.Assert(t, qt.IsNil(err))
	v, err = pkg.Value(ctxbg)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(lookupInt(t, v, "y"), int64(2)))
}

func extFiles() map[string]string {
	files := baseFiles()
	files["work/cue.mod/module.cue"] = `module: "main.example@v0"
language: version: "v0.9.0"
deps: "dep.example@v0": {v: "v0.1.0", default: true}
`
	files["work/ext/ext.cue"] = `package ext

import "dep.example/bar"

answer: bar.answer
`
	return files
}

func TestHermeticFailure(t *testing.T) {
	// With a zero-config (nil) registry, imports outside the main
	// module and the standard library must fail.
	l := newLoader(t, extFiles(), nil)
	pkg, err := l.Package(ctxbg, "./ext")
	qt.Assert(t, qt.IsNil(err))
	v, err := pkg.Value(ctxbg)
	qt.Assert(t, qt.IsNil(err))
	err = v.Validate(ctxbg)
	qt.Assert(t, qt.ErrorMatches(err, "(?s).*no module registry configured.*"))

	// The dependency package itself reports the failure.
	imports := pkg.Imports()
	qt.Assert(t, qt.HasLen(imports, 1))
	qt.Assert(t, qt.ErrorMatches(imports[0].Err(), "(?s).*no module registry configured.*"))
}

// fakeRegistry is an in-memory modconfig.Registry serving fixed module
// contents.
type fakeRegistry struct {
	mods map[string]module.SourceLoc // "path version" -> location
}

func (r *fakeRegistry) Fetch(ctx context.Context, m module.Version) (module.SourceLoc, error) {
	loc, ok := r.mods[m.Path()+" "+m.Version()]
	if !ok {
		return module.SourceLoc{}, fmt.Errorf("module %v not found in fake registry", m)
	}
	return loc, nil
}

func (r *fakeRegistry) ModFile(ctx context.Context, m module.Version) (*modfile.File, error) {
	loc, err := r.Fetch(ctx, m)
	if err != nil {
		return nil, err
	}
	data, err := fs.ReadFile(loc.FS, path.Join(loc.Dir, "cue.mod/module.cue"))
	if err != nil {
		return nil, err
	}
	return modfile.Parse(data, "cue.mod/module.cue")
}

func (r *fakeRegistry) ModuleVersions(ctx context.Context, mpath string) ([]string, error) {
	var versions []string
	for k := range r.mods {
		p, v, _ := strings.Cut(k, " ")
		if p == mpath {
			versions = append(versions, v)
		}
	}
	slices.Sort(versions)
	return versions, nil
}

func TestRegistryDeps(t *testing.T) {
	depFS := testFS(map[string]string{
		"cue.mod/module.cue": `module: "dep.example@v0"
language: version: "v0.9.0"
`,
		"bar/bar.cue": "package bar\n\nanswer: 42\n",
	})
	reg := &fakeRegistry{mods: map[string]module.SourceLoc{
		"dep.example@v0 v0.1.0": {FS: depFS, Dir: "."},
	}}
	l := newLoader(t, extFiles(), func(cfg *cueload.Config) {
		cfg.Registry = reg
	})
	pkg, err := l.Package(ctxbg, "./ext")
	qt.Assert(t, qt.IsNil(err))
	v, err := pkg.Value(ctxbg)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsNil(v.Validate(ctxbg)))
	qt.Assert(t, qt.Equals(lookupInt(t, v, "answer"), int64(42)))

	imports := pkg.Imports()
	qt.Assert(t, qt.HasLen(imports, 1))
	dep := imports[0]
	qt.Assert(t, qt.IsNil(dep.Err()))
	qt.Assert(t, qt.IsNotNil(dep.Module()))
	qt.Assert(t, qt.Equals(dep.Module().Path(), "dep.example@v0"))
	qt.Assert(t, qt.Equals(dep.Module().Version().Version(), "v0.1.0"))
}

func TestCancellation(t *testing.T) {
	l := newLoader(t, baseFiles(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var seen bool
	for _, err := range l.Load(ctx, cueload.Pkg("./bar")) {
		qt.Assert(t, qt.ErrorIs(err, context.Canceled))
		seen = true
	}
	qt.Assert(t, qt.IsTrue(seen))
}

func TestModulePackages(t *testing.T) {
	l := newLoader(t, baseFiles(), nil)
	pkg, err := l.Package(ctxbg, "./bar")
	qt.Assert(t, qt.IsNil(err))
	m := pkg.Module()
	qt.Assert(t, qt.IsNotNil(m))

	var paths []string
	for p, err := range m.Packages(ctxbg) {
		qt.Assert(t, qt.IsNil(err))
		paths = append(paths, p.ImportPath().String())
	}
	qt.Assert(t, qt.DeepEquals(paths, []string{
		"main.example/bar@v0:bar",
		"main.example/hello@v0:hello",
		"main.example/util@v0:util",
	}))
}

func TestWildcardPattern(t *testing.T) {
	l := newLoader(t, baseFiles(), nil)
	pkgs, err := l.Packages(ctxbg, "./...")
	qt.Assert(t, qt.IsNil(err))
	var names []string
	for _, p := range pkgs {
		names = append(names, p.Name())
	}
	qt.Assert(t, qt.DeepEquals(names, []string{"bar", "hello", "util"}))

	// Text after ... is not supported.
	_, err = l.Packages(ctxbg, "./...x")
	qt.Assert(t, qt.ErrorMatches(err, ".*text after \\.\\.\\. is not supported.*"))

	// An unmatched wildcard is an error.
	_, err = l.Packages(ctxbg, "./hello/nothing/...")
	qt.Assert(t, qt.ErrorMatches(err, ".*no packages matched pattern.*"))
}

func TestSourceString(t *testing.T) {
	schemaFile, err := parser.ParseFile("s.cue", "a: int\n")
	qt.Assert(t, qt.IsNil(err))
	src := cueload.Validate(
		cueload.Unify(
			cueload.Decode(cueload.File{Name: "d.yaml"}),
			cueload.Pkg("./schema"),
		),
		cueload.Syntax(schemaFile),
		cueload.Concrete(true),
	)
	qt.Assert(t, qt.Equals(src.String(),
		`validate(unify(decode("d.yaml"), pkg("./schema")), syntax("s.cue"), concrete)`))

	expr, err := parser.ParseExpr("e", "a+b")
	qt.Assert(t, qt.IsNil(err))
	src2 := cueload.Eval(cueload.AsList(cueload.Concat(cueload.Go(1), cueload.Pkg("foo.example/x"))), expr)
	qt.Assert(t, qt.Equals(src2.String(),
		`eval(list(concat(go(int), pkg("foo.example/x"))), a + b)`))
}

func TestNoModule(t *testing.T) {
	l := newLoader(t, map[string]string{
		"work/x.cue": "a: 1\n",
	}, nil)
	_, err := l.Packages(ctxbg, "./x")
	qt.Assert(t, qt.ErrorMatches(err, ".*no CUE module found.*"))

	// Building syntax without imports still works.
	f, err := parser.ParseFile("x.cue", "a: 1\n")
	qt.Assert(t, qt.IsNil(err))
	v, err := l.Build(ctxbg, f)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(lookupInt(t, v, "a"), int64(1)))
}

func TestStatsRecorder(t *testing.T) {
	rec := &stats.Recorder{}
	l := newLoader(t, baseFiles(), func(cfg *cueload.Config) {
		cfg.Evaluator = &cueload.EvaluatorConfig{Recorder: rec}
	})
	pkg, err := l.Package(ctxbg, "./hello")
	qt.Assert(t, qt.IsNil(err))
	_, err = pkg.Value(ctxbg)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsTrue(rec.Counts().Unifications > 0))
}

func TestConcurrentUse(t *testing.T) {
	l := newLoader(t, baseFiles(), nil)
	errc := make(chan error)
	for i := range 8 {
		go func() {
			errc <- func() error {
				if i%2 == 0 {
					pkg, err := l.Package(ctxbg, "./hello")
					if err != nil {
						return err
					}
					v, err := pkg.Value(ctxbg)
					if err != nil {
						return err
					}
					s, err := v.LookupPath(cue.ParsePath("greeting")).AsString(ctxbg)
					if err != nil {
						return err
					}
					if s != "hello, world" {
						return fmt.Errorf("unexpected greeting %q", s)
					}
					return nil
				}
				n := 0
				for _, err := range l.Load(ctxbg, cueload.Decode(cueload.File{Name: "data.yaml"})) {
					if err != nil {
						return err
					}
					n++
				}
				if n != 3 {
					return fmt.Errorf("got %d documents, want 3", n)
				}
				return nil
			}()
		}()
	}
	for range 8 {
		qt.Assert(t, qt.IsNil(<-errc))
	}
}

func TestZeroSource(t *testing.T) {
	l := newLoader(t, baseFiles(), nil)
	_, err := l.LoadValue(ctxbg, cueload.Source{})
	qt.Assert(t, qt.ErrorMatches(err, ".*invalid \\(zero\\) Source.*"))
}
