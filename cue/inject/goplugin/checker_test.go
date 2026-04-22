package goplugin_test

import (
	"fmt"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/inject/goplugin"
	"cuelang.org/go/mod/module"

	qt "github.com/go-quicktest/qt"
)

func TestCheckerAllowed(t *testing.T) {
	manifest := &goplugin.Manifest{
		Modules: map[string]goplugin.Module{
			"test.example@v0": {
				Instances: map[string]goplugin.Instance{
					"test.example@v0:example": {
						GoPackages: map[string][]string{
							"example.com/pkg": {"Greet"},
						},
					},
				},
			},
		},
		GoPackages: map[string]goplugin.GoModule{
			"example.com/pkg": {
				Path:    "example.com/pkg",
				Version: "v1.0.0",
			},
		},
	}
	refMap := map[goplugin.Reference]func() cue.Value{
		{GoPackage: "example.com/pkg", CUEModuleVersion: "test.example@v0", Name: "Greet"}: func() cue.Value {
			return cueValue(`"hello"`)
		},
	}
	called := 0
	check := func(ref goplugin.ResolvedReference) error {
		called++
		return nil
	}
	ctx := cuecontext.New(
		cuecontext.WithInjection(goplugin.Checker(manifest, check)),
		cuecontext.WithInjection(goplugin.Injection(refMap)),
	)
	v := buildInstance(t, ctx, "test.example@v0", `
		@extern(go, import=("example.com/pkg"))

		package example

		x: _ @go(pkg.Greet)
	`)
	qt.Assert(t, qt.IsNil(v.Err()))
	qt.Assert(t, qt.Equals(called, 1))

	got, err := v.LookupPath(cue.ParsePath("x")).String()
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Equals(got, "hello"))
}

func TestCheckerDenied(t *testing.T) {
	manifest := &goplugin.Manifest{
		Modules: map[string]goplugin.Module{
			"test.example@v0": {
				Instances: map[string]goplugin.Instance{
					"test.example@v0:example": {
						GoPackages: map[string][]string{
							"example.com/pkg": {"Greet"},
						},
					},
				},
			},
		},
		GoPackages: map[string]goplugin.GoModule{
			"example.com/pkg": {
				Path:    "example.com/pkg",
				Version: "v1.0.0",
			},
		},
	}
	refMap := map[goplugin.Reference]func() cue.Value{
		{GoPackage: "example.com/pkg", CUEModuleVersion: "test.example@v0", Name: "Greet"}: func() cue.Value {
			return cueValue(`"hello"`)
		},
	}
	check := func(ref goplugin.ResolvedReference) error {
		return fmt.Errorf("Go package %q from module %s is not allowed", ref.GoImportPath, ref.GoModule.Path)
	}
	ctx := cuecontext.New(
		cuecontext.WithInjection(goplugin.Checker(manifest, check)),
		cuecontext.WithInjection(goplugin.Injection(refMap)),
	)
	v := buildInstance(t, ctx, "test.example@v0", `
		@extern(go, import=("example.com/pkg"))

		package example

		x: _ @go(pkg.Greet)
	`)
	qt.Assert(t, qt.ErrorMatches(v.Err(), `.*Go package "example.com/pkg" from module example.com/pkg is not allowed.*`))
}

func TestCheckerColocatedPackage(t *testing.T) {
	manifest := &goplugin.Manifest{
		Modules: map[string]goplugin.Module{
			"test.example@v0": {
				Instances: map[string]goplugin.Instance{
					"test.example@v0:example": {
						ColocatedGoPackage: "github.com/user/example",
						GoPackages: map[string][]string{
							"github.com/user/example": {"LocalFunc"},
						},
					},
				},
			},
		},
		GoPackages: map[string]goplugin.GoModule{
			"github.com/user/example": {
				Path:    "github.com/user/example",
				Version: "v0.1.0",
			},
		},
	}
	refMap := map[goplugin.Reference]func() cue.Value{
		{
			GoPackage:        ".",
			CUEModuleVersion: "test.example@v0",
			CUEInstance:      "test.example@v0:example",
			Name:             "LocalFunc",
		}: func() cue.Value {
			return cueValue("42")
		},
	}
	var got goplugin.ResolvedReference
	check := func(ref goplugin.ResolvedReference) error {
		got = ref
		return nil
	}
	ctx := cuecontext.New(
		cuecontext.WithInjection(goplugin.Checker(manifest, check)),
		cuecontext.WithInjection(goplugin.Injection(refMap)),
	)
	v := buildInstance(t, ctx, "test.example@v0", `
		@extern(go)

		package example

		x: _ @go(LocalFunc)
	`)
	qt.Assert(t, qt.IsNil(v.Err()))
	qt.Assert(t, qt.DeepEquals(got, goplugin.ResolvedReference{
		CUEModule:    module.MustNewVersion("test.example@v0", ""),
		GoImportPath: "github.com/user/example",
		GoModule: goplugin.GoModule{
			Path:    "github.com/user/example",
			Version: "v0.1.0",
		},
		Name:          "LocalFunc",
		CUEImportPath: "test.example@v0:example",
	}))
}

func TestCheckerDirReplace(t *testing.T) {
	manifest := &goplugin.Manifest{
		Modules: map[string]goplugin.Module{
			"test.example@v0": {
				Instances: map[string]goplugin.Instance{
					"test.example@v0:example": {
						GoPackages: map[string][]string{
							"example.com/pkg": {"Func"},
						},
					},
				},
			},
		},
		GoPackages: map[string]goplugin.GoModule{
			"example.com/pkg": {
				Path: "example.com/pkg",
				Dir:  "../local/pkg",
			},
		},
	}
	refMap := map[goplugin.Reference]func() cue.Value{
		{GoPackage: "example.com/pkg", CUEModuleVersion: "test.example@v0", Name: "Func"}: func() cue.Value {
			return cueValue(`"local"`)
		},
	}
	var got goplugin.ResolvedReference
	check := func(ref goplugin.ResolvedReference) error {
		got = ref
		return nil
	}
	ctx := cuecontext.New(
		cuecontext.WithInjection(goplugin.Checker(manifest, check)),
		cuecontext.WithInjection(goplugin.Injection(refMap)),
	)
	v := buildInstance(t, ctx, "test.example@v0", `
		@extern(go, import=("example.com/pkg"))

		package example

		x: _ @go(pkg.Func)
	`)
	qt.Assert(t, qt.IsNil(v.Err()))
	qt.Assert(t, qt.DeepEquals(got, goplugin.ResolvedReference{
		CUEModule:     module.MustNewVersion("test.example@v0", ""),
		CUEImportPath: "test.example@v0:example",
		GoModule: goplugin.GoModule{
			Path: "example.com/pkg",
			Dir:  "../local/pkg",
		},
		GoImportPath: "example.com/pkg",
		Name:         "Func",
	}))
}

func TestCheckerMissingModule(t *testing.T) {
	manifest := &goplugin.Manifest{
		Modules: map[string]goplugin.Module{},
	}
	check := func(ref goplugin.ResolvedReference) error {
		return nil
	}
	ctx := cuecontext.New(
		cuecontext.WithInjection(goplugin.Checker(manifest, check)),
	)
	v := buildInstance(t, ctx, "test.example@v0", `
		@extern(go, import=("example.com/pkg"))

		package example

		x: _ @go(pkg.Func)
	`)
	qt.Assert(t, qt.ErrorMatches(v.Err(), `.*CUE module "test.example@v0" not found in manifest.*`))
}

func TestCheckerMissingInstance(t *testing.T) {
	manifest := &goplugin.Manifest{
		Modules: map[string]goplugin.Module{
			"test.example@v0": {
				Instances: map[string]goplugin.Instance{},
			},
		},
	}
	check := func(ref goplugin.ResolvedReference) error {
		return nil
	}
	ctx := cuecontext.New(
		cuecontext.WithInjection(goplugin.Checker(manifest, check)),
	)
	v := buildInstance(t, ctx, "test.example@v0", `
		@extern(go, import=("example.com/pkg"))

		package example

		x: _ @go(pkg.Func)
	`)
	qt.Assert(t, qt.ErrorMatches(v.Err(), `.*CUE instance "test.example@v0:example" not found in manifest.*`))
}

func TestCheckerMissingGoPackage(t *testing.T) {
	manifest := &goplugin.Manifest{
		Modules: map[string]goplugin.Module{
			"test.example@v0": {
				Instances: map[string]goplugin.Instance{
					"test.example@v0:example": {
						GoPackages: map[string][]string{},
					},
				},
			},
		},
	}
	check := func(ref goplugin.ResolvedReference) error {
		return nil
	}
	ctx := cuecontext.New(
		cuecontext.WithInjection(goplugin.Checker(manifest, check)),
	)
	v := buildInstance(t, ctx, "test.example@v0", `
		@extern(go, import=("example.com/pkg"))

		package example

		x: _ @go(pkg.Func)
	`)
	qt.Assert(t, qt.ErrorMatches(v.Err(), `.*Go package "example.com/pkg" not found in manifest.*`))
}

func TestCheckerMissingColocatedGoPackage(t *testing.T) {
	manifest := &goplugin.Manifest{
		Modules: map[string]goplugin.Module{
			"test.example@v0": {
				Instances: map[string]goplugin.Instance{
					"test.example@v0:example": {
						GoPackages: map[string][]string{},
					},
				},
			},
		},
	}
	check := func(ref goplugin.ResolvedReference) error {
		return nil
	}
	ctx := cuecontext.New(
		cuecontext.WithInjection(goplugin.Checker(manifest, check)),
	)
	v := buildInstance(t, ctx, "test.example@v0", `
		@extern(go)

		package example

		x: _ @go(Func)
	`)
	qt.Assert(t, qt.ErrorMatches(v.Err(), `.*no co-located Go package for CUE instance.*`))
}

func TestCheckerMissingGoModule(t *testing.T) {
	manifest := &goplugin.Manifest{
		Modules: map[string]goplugin.Module{
			"test.example@v0": {
				Instances: map[string]goplugin.Instance{
					"test.example@v0:example": {
						GoPackages: map[string][]string{
							"example.com/pkg": {"Func"},
						},
					},
				},
			},
		},
		GoPackages: map[string]goplugin.GoModule{},
	}
	check := func(ref goplugin.ResolvedReference) error {
		return nil
	}
	ctx := cuecontext.New(
		cuecontext.WithInjection(goplugin.Checker(manifest, check)),
	)
	v := buildInstance(t, ctx, "test.example@v0", `
		@extern(go, import=("example.com/pkg"))

		package example

		x: _ @go(pkg.Func)
	`)
	qt.Assert(t, qt.ErrorMatches(v.Err(), `.*Go module for package "example.com/pkg" not found in manifest.*`))
}
