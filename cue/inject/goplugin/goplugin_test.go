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

package goplugin_test

import (
	"fmt"
	"path/filepath"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/inject/goplugin"
	"cuelang.org/go/cue/load"

	qt "github.com/go-quicktest/qt"
)

func TestQualifiedRef(t *testing.T) {
	refMap := map[goplugin.Reference]func() cue.Value{
		{GoPackage: "example.com/pkg", Name: "Greet"}: func() cue.Value {
			return cueValue(`"hello"`)
		},
	}
	ctx := cuecontext.New(cuecontext.WithInjection(goplugin.Injection(refMap)))

	v := ctx.CompileString(`
		@extern(go, import=("example.com/pkg"))

		package foo

		x: _ @go(pkg.Greet)
	`)
	qt.Assert(t, qt.IsNil(v.Err()))

	got, err := v.LookupPath(cue.ParsePath("x")).String()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(got, "hello"))
}

func TestAliasedImport(t *testing.T) {
	refMap := map[goplugin.Reference]func() cue.Value{
		{GoPackage: "example.com/some/longname", Name: "Func"}: func() cue.Value {
			return cueValue("42")
		},
	}
	ctx := cuecontext.New(cuecontext.WithInjection(goplugin.Injection(refMap)))

	v := ctx.CompileString(`
		@extern(go, import=(short "example.com/some/longname"))

		package foo

		x: _ @go(short.Func)
	`)

	qt.Assert(t, qt.IsNil(v.Err()))

	got, err := v.LookupPath(cue.ParsePath("x")).Int64()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(got, int64(42)))
}

func TestMultipleImports(t *testing.T) {
	refMap := map[goplugin.Reference]func() cue.Value{
		{GoPackage: "example.com/alpha", Name: "A"}: func() cue.Value {
			return cueValue(`"from alpha"`)
		},
		{GoPackage: "example.com/beta", Name: "B"}: func() cue.Value {
			return cueValue(`"from beta"`)
		},
	}
	ctx := cuecontext.New(cuecontext.WithInjection(goplugin.Injection(refMap)))

	v := ctx.CompileString(`
		@extern(go, import=(
			"example.com/alpha"
			"example.com/beta"
		))

		package foo

		a: _ @go(alpha.A)
		b: _ @go(beta.B)
	`)
	qt.Assert(t, qt.IsNil(v.Err()))

	gotA, err := v.LookupPath(cue.ParsePath("a")).String()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(gotA, "from alpha"))

	gotB, err := v.LookupPath(cue.ParsePath("b")).String()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(gotB, "from beta"))
}

func TestEmptyRef(t *testing.T) {
	refMap := map[goplugin.Reference]func() cue.Value{
		{
			GoPackage:        ".",
			CUEModuleVersion: "test.example@v0",
			CUEInstance:      "test.example@v0:example",
			Name:             "Foo",
		}: func() cue.Value {
			return cueValue(`"local foo"`)
		},
	}
	ctx := cuecontext.New(cuecontext.WithInjection(goplugin.Injection(refMap)))

	v := buildInstance(t, ctx, "test.example@v0", `
		@extern(go)

		package example

		Foo: _ @go()
	`)
	qt.Assert(t, qt.IsNil(v.Err()))

	got, err := v.LookupPath(cue.ParsePath("Foo")).String()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(got, "local foo"))
}

func TestUnqualifiedRef(t *testing.T) {
	refMap := map[goplugin.Reference]func() cue.Value{
		{
			GoPackage:        ".",
			CUEModuleVersion: "test.example@v0",
			CUEInstance:      "test.example@v0:example",
			Name:             "Bar",
		}: func() cue.Value {
			return cueValue(`"local bar"`)
		},
	}
	ctx := cuecontext.New(cuecontext.WithInjection(goplugin.Injection(refMap)))

	v := buildInstance(t, ctx, "test.example@v0", `
		@extern(go)

		package example

		x: _ @go(Bar)
	`)
	qt.Assert(t, qt.IsNil(v.Err()))

	got, err := v.LookupPath(cue.ParsePath("x")).String()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(got, "local bar"))
}

func TestNoExternIgnored(t *testing.T) {
	ctx := cuecontext.New(cuecontext.WithInjection(goplugin.Injection(nil)))

	// Without @extern(go), the @go attribute should be ignored.
	v := ctx.CompileString(`
		package foo

		x: "original" @go(Foo)
	`)

	qt.Assert(t, qt.IsNil(v.Err()))

	got, err := v.LookupPath(cue.ParsePath("x")).String()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(got, "original"))
}

func TestLocalRefWithoutModule(t *testing.T) {
	ctx := cuecontext.New(cuecontext.WithInjection(goplugin.Injection(nil)))
	v := ctx.CompileString(`
		@extern(go)

		package foo

		x: _ @go(Func)
	`)
	qt.Assert(t, qt.ErrorMatches(v.Err(), `.*local Go plugin reference requires a CUE module.*`))
}

func TestMissingPlugin(t *testing.T) {
	ctx := cuecontext.New(cuecontext.WithInjection(goplugin.Injection(nil)))
	v := ctx.CompileString(`
		@extern(go, import=("example.com/pkg"))

		package foo

		x: _ @go(pkg.Missing)
	`)

	qt.Assert(t, qt.ErrorMatches(v.Err(), `.*no Go plugin registered.*`))
}

func TestUnknownImportIdent(t *testing.T) {
	ctx := cuecontext.New(cuecontext.WithInjection(goplugin.Injection(nil)))
	v := ctx.CompileString(`
		@extern(go, import=("example.com/pkg"))

		package foo

		x: _ @go(unknown.Func)
	`)
	qt.Assert(t, qt.ErrorMatches(v.Err(), `.*unknown import identifier "unknown".*`))
}

func TestV2ModulePath(t *testing.T) {
	refMap := map[goplugin.Reference]func() cue.Value{
		{GoPackage: "example.com/foo/bar/v2", Name: "Func"}: func() cue.Value {
			return cueValue(`"v2 value"`)
		},
	}
	ctx := cuecontext.New(cuecontext.WithInjection(goplugin.Injection(refMap)))

	// The default identifier for "example.com/foo/bar/v2" should be "bar", not "v2".
	v := ctx.CompileString(`
		@extern(go, import=("example.com/foo/bar/v2"))

		package foo

		x: _ @go(bar.Func)
	`)
	qt.Assert(t, qt.IsNil(v.Err()))

	got, err := v.LookupPath(cue.ParsePath("x")).String()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(got, "v2 value"))
}

func TestDuplicateImportIdent(t *testing.T) {
	ctx := cuecontext.New(cuecontext.WithInjection(goplugin.Injection(nil)))
	v := ctx.CompileString(`
		@extern(go, import=(
			"example.com/foo/pkg"
			"example.com/bar/pkg"
		))

		package foo

		x: _ @go(pkg.Func)
	`)
	qt.Assert(t, qt.ErrorMatches(v.Err(), `.*duplicate import identifier "pkg".*`))
}

func TestDotImport(t *testing.T) {
	ctx := cuecontext.New(cuecontext.WithInjection(goplugin.Injection(nil)))
	v := ctx.CompileString(`
		@extern(go, import=(. "example.com/pkg"))

		package foo

		x: _ @go(Func)
	`)
	qt.Assert(t, qt.ErrorMatches(v.Err(), `.*unsupported import name.*`))
}

func TestBlankImport(t *testing.T) {
	ctx := cuecontext.New(cuecontext.WithInjection(goplugin.Injection(nil)))
	v := ctx.CompileString(`
		@extern(go, import=(_ "example.com/pkg"))

		package foo

		x: _ @go(Func)
	`)
	qt.Assert(t, qt.ErrorMatches(v.Err(), `.*unsupported import name.*`))
}

func TestInjectFunction(t *testing.T) {
	refMap := map[goplugin.Reference]func() cue.Value{
		{GoPackage: "example.com/strutil", Name: "Greet"}: func() cue.Value {
			return cue.NewPureFunc1(func(s string) (string, error) {
				return "hello, " + s, nil
			})
		},
	}
	ctx := cuecontext.New(cuecontext.WithInjection(goplugin.Injection(refMap)))
	v := ctx.CompileString(`
		@extern(go, import=("example.com/strutil"))

		package foo

		Greet: _ @go(strutil.Greet)
		x: Greet("world")
	`)

	qt.Assert(t, qt.IsNil(v.Err()))

	got := fmt.Sprint(v.LookupPath(cue.ParsePath("x")))
	qt.Assert(t, qt.Equals(got, `"hello, world"`))
}

func TestMixedLocalAndImported(t *testing.T) {
	refMap := map[goplugin.Reference]func() cue.Value{
		{
			GoPackage:        "example.com/ext",
			CUEModuleVersion: "test.example@v0",
			Name:             "ExtVal",
		}: func() cue.Value {
			return cueValue(`"external"`)
		},
		{
			GoPackage:        ".",
			CUEModuleVersion: "test.example@v0",
			CUEInstance:      "test.example@v0:example",
			Name:             "LocalVal",
		}: func() cue.Value {
			return cueValue(`"local"`)
		},
	}
	ctx := cuecontext.New(cuecontext.WithInjection(goplugin.Injection(refMap)))

	v := buildInstance(t, ctx, "test.example@v0", `
		@extern(go, import=("example.com/ext"))

		package example

		a: _ @go(ext.ExtVal)
		b: _ @go(LocalVal)
	`)
	qt.Assert(t, qt.IsNil(v.Err()))

	gotA, err := v.LookupPath(cue.ParsePath("a")).String()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(gotA, "external"))

	gotB, err := v.LookupPath(cue.ParsePath("b")).String()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(gotB, "local"))
}

func TestStructValue(t *testing.T) {
	refMap := map[goplugin.Reference]func() cue.Value{
		{GoPackage: "example.com/pkg", Name: "Config"}: func() cue.Value {
			return cueValue(`{host: "localhost", port: 8080}`)
		},
	}
	ctx := cuecontext.New(cuecontext.WithInjection(goplugin.Injection(refMap)))
	v := ctx.CompileString(`
		@extern(go, import=("example.com/pkg"))

		package foo

		x: _ @go(pkg.Config)
	`)
	qt.Assert(t, qt.IsNil(v.Err()))

	host, err := v.LookupPath(cue.ParsePath("x.host")).String()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(host, "localhost"))

	port, err := v.LookupPath(cue.ParsePath("x.port")).Int64()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(port, int64(8080)))
}

func TestInvalidImportSyntax(t *testing.T) {
	ctx := cuecontext.New(cuecontext.WithInjection(goplugin.Injection(nil)))
	v := ctx.CompileString(`
		@extern(go, import=(not valid go))

		package foo

		x: _ @go(Func)
	`)
	qt.Assert(t, qt.ErrorMatches(v.Err(), `.*cannot parse Go import declaration.*`))
}

// buildInstance loads and builds a single CUE instance from a module
// with the given module path and CUE source.
func buildInstance(t *testing.T, ctx *cue.Context, modulePath, cueSrc string) cue.Value {
	t.Helper()
	dir := t.TempDir()
	cfg := &load.Config{
		Dir: dir,
		Overlay: map[string]load.Source{
			filepath.Join(dir, "cue.mod", "module.cue"): load.FromString(fmt.Sprintf(`
module: %q
language: version: "v0.12.0"
`, modulePath)),
			filepath.Join(dir, "x.cue"): load.FromString(cueSrc),
		},
	}
	insts := load.Instances([]string{"."}, cfg)
	qt.Assert(t, qt.IsNil(insts[0].Err))
	return ctx.BuildInstance(insts[0])
}

func TestReferencesQualified(t *testing.T) {
	inst := loadInstance(t, "test.example@v0", `
		@extern(go, import=("example.com/pkg"))

		package example

		x: _ @go(pkg.Greet)
	`)
	var got []goplugin.Reference
	for ref, err := range goplugin.References(inst) {
		qt.Assert(t, qt.IsNil(err))
		got = append(got, ref)
	}
	qt.Assert(t, qt.DeepEquals(got, []goplugin.Reference{{
		GoPackage:        "example.com/pkg",
		CUEModuleVersion: "test.example@v0",
		Name:             "Greet",
	}}))
}

func TestReferencesUnqualified(t *testing.T) {
	inst := loadInstance(t, "test.example@v0", `
		@extern(go)

		package example

		x: _ @go(Bar)
	`)
	var got []goplugin.Reference
	for ref, err := range goplugin.References(inst) {
		qt.Assert(t, qt.IsNil(err))
		got = append(got, ref)
	}
	qt.Assert(t, qt.DeepEquals(got, []goplugin.Reference{{
		GoPackage:        ".",
		CUEModuleVersion: "test.example@v0",
		CUEInstance:      "test.example@v0:example",
		Name:             "Bar",
	}}))
}

func TestReferencesEmptyRef(t *testing.T) {
	inst := loadInstance(t, "test.example@v0", `
		@extern(go)

		package example

		Foo: _ @go()
	`)
	var got []goplugin.Reference
	for ref, err := range goplugin.References(inst) {
		qt.Assert(t, qt.IsNil(err))
		got = append(got, ref)
	}
	qt.Assert(t, qt.DeepEquals(got, []goplugin.Reference{{
		GoPackage:        ".",
		CUEModuleVersion: "test.example@v0",
		CUEInstance:      "test.example@v0:example",
		Name:             "Foo",
	}}))
}

func TestReferencesMultipleImports(t *testing.T) {
	inst := loadInstance(t, "test.example@v0", `
		@extern(go, import=(
			"example.com/alpha"
			"example.com/beta"
		))

		package example

		a: _ @go(alpha.A)
		b: _ @go(beta.B)
	`)
	var got []goplugin.Reference
	for ref, err := range goplugin.References(inst) {
		qt.Assert(t, qt.IsNil(err))
		got = append(got, ref)
	}
	qt.Assert(t, qt.DeepEquals(got, []goplugin.Reference{{
		GoPackage:        "example.com/alpha",
		CUEModuleVersion: "test.example@v0",
		Name:             "A",
	}, {
		GoPackage:        "example.com/beta",
		CUEModuleVersion: "test.example@v0",
		Name:             "B",
	}}))
}

func TestReferencesAliasedImport(t *testing.T) {
	inst := loadInstance(t, "test.example@v0", `
		@extern(go, import=(short "example.com/some/longname"))

		package example

		x: _ @go(short.Func)
	`)
	var got []goplugin.Reference
	for ref, err := range goplugin.References(inst) {
		qt.Assert(t, qt.IsNil(err))
		got = append(got, ref)
	}
	qt.Assert(t, qt.DeepEquals(got, []goplugin.Reference{{
		GoPackage:        "example.com/some/longname",
		CUEModuleVersion: "test.example@v0",
		Name:             "Func",
	}}))
}

func TestReferencesV2ModulePath(t *testing.T) {
	inst := loadInstance(t, "test.example@v0", `
		@extern(go, import=("example.com/foo/bar/v2"))

		package example

		x: _ @go(bar.Func)
	`)
	var got []goplugin.Reference
	for ref, err := range goplugin.References(inst) {
		qt.Assert(t, qt.IsNil(err))
		got = append(got, ref)
	}
	qt.Assert(t, qt.DeepEquals(got, []goplugin.Reference{{
		GoPackage:        "example.com/foo/bar/v2",
		CUEModuleVersion: "test.example@v0",
		Name:             "Func",
	}}))
}

func TestReferencesMixedLocalAndImported(t *testing.T) {
	inst := loadInstance(t, "test.example@v0", `
		@extern(go, import=("example.com/ext"))

		package example

		a: _ @go(ext.ExtVal)
		b: _ @go(LocalVal)
	`)
	var got []goplugin.Reference
	for ref, err := range goplugin.References(inst) {
		qt.Assert(t, qt.IsNil(err))
		got = append(got, ref)
	}
	qt.Assert(t, qt.DeepEquals(got, []goplugin.Reference{{
		GoPackage:        "example.com/ext",
		CUEModuleVersion: "test.example@v0",
		Name:             "ExtVal",
	}, {
		GoPackage:        ".",
		CUEModuleVersion: "test.example@v0",
		CUEInstance:      "test.example@v0:example",
		Name:             "LocalVal",
	}}))
}

func TestReferencesNoExtern(t *testing.T) {
	inst := loadInstance(t, "test.example@v0", `
		package example

		x: "original" @go(Foo)
	`)
	var got []goplugin.Reference
	for ref, err := range goplugin.References(inst) {
		qt.Assert(t, qt.IsNil(err))
		got = append(got, ref)
	}
	qt.Assert(t, qt.DeepEquals(got, []goplugin.Reference(nil)))
}

func TestReferencesUnknownImportIdent(t *testing.T) {
	inst := loadInstance(t, "test.example@v0", `
		@extern(go, import=("example.com/pkg"))

		package example

		x: _ @go(unknown.Func)
	`)
	var errs []string
	for _, err := range goplugin.References(inst) {
		if err != nil {
			errs = append(errs, err.Error())
		}
	}
	qt.Assert(t, qt.HasLen(errs, 1))
	qt.Assert(t, qt.Matches(errs[0], `.*unknown import identifier "unknown".*`))
}

func TestReferencesInvalidImportSyntax(t *testing.T) {
	inst := loadInstance(t, "test.example@v0", `
		@extern(go, import=(not valid go))

		package example

		x: _ @go(Func)
	`)
	var errs []string
	for _, err := range goplugin.References(inst) {
		if err != nil {
			errs = append(errs, err.Error())
		}
	}
	qt.Assert(t, qt.HasLen(errs, 1))
	qt.Assert(t, qt.Matches(errs[0], `.*cannot parse Go import declaration.*`))
}

func TestReferencesMultipleFiles(t *testing.T) {
	dir := t.TempDir()
	cfg := &load.Config{
		Dir: dir,
		Overlay: map[string]load.Source{
			filepath.Join(dir, "cue.mod", "module.cue"): load.FromString(`
module: "test.example@v0"
language: version: "v0.12.0"
`),
			filepath.Join(dir, "a.cue"): load.FromString(`
@extern(go, import=("example.com/alpha"))

package example

a: _ @go(alpha.A)
`),
			filepath.Join(dir, "b.cue"): load.FromString(`
@extern(go, import=("example.com/beta"))

package example

b: _ @go(beta.B)
`),
		},
	}
	insts := load.Instances([]string{"."}, cfg)
	qt.Assert(t, qt.IsNil(insts[0].Err))

	refMap := make(map[goplugin.Reference]bool)
	for ref, err := range goplugin.References(insts[0]) {
		qt.Assert(t, qt.IsNil(err))
		refMap[ref] = true
	}
	qt.Assert(t, qt.IsTrue(refMap[goplugin.Reference{
		GoPackage:        "example.com/alpha",
		CUEModuleVersion: "test.example@v0",
		Name:             "A",
	}]))
	qt.Assert(t, qt.IsTrue(refMap[goplugin.Reference{
		GoPackage:        "example.com/beta",
		CUEModuleVersion: "test.example@v0",
		Name:             "B",
	}]))
	qt.Assert(t, qt.Equals(len(refMap), 2))
}

func TestReferencesNonGoExternIgnored(t *testing.T) {
	inst := loadInstance(t, "test.example@v0", `
		@extern(embed)

		package example

		x: _ @embed(file="data.json")
	`)
	var got []goplugin.Reference
	for ref, err := range goplugin.References(inst) {
		qt.Assert(t, qt.IsNil(err))
		got = append(got, ref)
	}
	qt.Assert(t, qt.DeepEquals(got, []goplugin.Reference(nil)))
}

func TestReferencesDuplicateImportIdent(t *testing.T) {
	inst := loadInstance(t, "test.example@v0", `
		@extern(go, import=(
			"example.com/foo/pkg"
			"example.com/bar/pkg"
		))

		package example

		x: _ @go(pkg.Func)
	`)
	var errs []string
	for _, err := range goplugin.References(inst) {
		if err != nil {
			errs = append(errs, err.Error())
		}
	}
	qt.Assert(t, qt.HasLen(errs, 1))
	qt.Assert(t, qt.Matches(errs[0], `.*duplicate import identifier "pkg".*`))
}

// loadInstance loads a build.Instance from a module with the given
// module path and CUE source.
func loadInstance(t *testing.T, modulePath, cueSrc string) *build.Instance {
	t.Helper()
	dir := t.TempDir()
	cfg := &load.Config{
		Dir: dir,
		Overlay: map[string]load.Source{
			filepath.Join(dir, "cue.mod", "module.cue"): load.FromString(fmt.Sprintf(`
module: %q
language: version: "v0.12.0"
`, modulePath)),
			filepath.Join(dir, "x.cue"): load.FromString(cueSrc),
		},
	}
	insts := load.Instances([]string{"."}, cfg)
	qt.Assert(t, qt.IsNil(insts[0].Err))
	return insts[0]
}

func cueValue(s string) cue.Value {
	v := cuecontext.New().CompileString(s)
	if err := v.Err(); err != nil {
		panic(err)
	}
	return v
}
