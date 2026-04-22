package goplugin

import (
	"fmt"

	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/mod/module"
)

// Manifest holds a list of all the references made by a given
// CUE main module. This is stored in the manifest file
// cue.mod/plugin/manifest.json and allows the goplugin
// package to efficiently resolve [Reference] values
// into [ResolvedReference] values in order to make
// [Checker] queries.
type Manifest struct {
	// Modules holds all the CUE modules that provide Go plugins.
	// The key is the CUE module path (e.g. "example.com@v0").
	Modules map[string]Module `json:"modules"`

	// GoPackages maps Go import paths to the Go module
	// that provides them. The Go module graph is shared across
	// all CUE modules in the build.
	GoPackages map[string]GoModule `json:"goPackages"`
}

// Module holds information about a CUE module that provides Go plugins.
type Module struct {
	// Instances holds the CUE instances (packages) that
	// provide a Go plugin, keyed by the full CUE import path.
	Instances map[string]Instance `json:"instances"`
}

// Instance holds information about a CUE instance that references Go plugins.
type Instance struct {
	// ColocatedGoPackage holds the Go import path of the
	// Go package co-located with this CUE instance.
	// It is used to resolve @go() references that use "."
	// as the Go package (i.e. unqualified references to the
	// co-located Go package).
	ColocatedGoPackage string `json:"colocatedGoPackage,omitempty"`

	// GoPackages holds the Go packages referenced by
	// the Go plugin references in the CUE instance.
	// The key is the Go import path and the value
	// holds the names referenced from that package.
	GoPackages map[string][]string `json:"goPackages"`
}

// GoModule holds information about a Go module.
type GoModule struct {
	// Path holds the Go module path.
	Path string `json:"path"`

	// Version holds the Go module version.
	// This is empty for directory replacements.
	Version string `json:"version,omitempty"`

	// Dir holds the replacement directory for directory replace directives.
	// This is empty when Version is set.
	Dir string `json:"dir,omitempty"`
}

// ResolvedReference holds information about a reference from a CUE instance
// to a Go plugin that has been fully resolved using a [Manifest].
type ResolvedReference struct {
	// CUEModule holds the CUE module that the reference is from.
	CUEModule module.Version

	// CUEImportPath holds the CUE import path of the instance
	// containing the reference.
	CUEImportPath string

	// GoModule holds the Go module that provides the referenced Go package.
	GoModule GoModule

	// GoImportPath holds the Go import path of the referenced package.
	GoImportPath string

	// Name holds the name of the referenced value within the Go package.
	Name string
}

// Checker returns a [cuecontext.Injection] that uses the given [Manifest] to
// resolve references made in CUE instances using @extern(go) to
// [ResolvedReference] values, and then calls the check function for each one.
// If the check function returns an error, the corresponding
// [runtime.Injector.InjectedValue] method will return an error;
// otherwise it will return top (_).
func Checker(m *Manifest, check func(ref ResolvedReference) error) cuecontext.Injection {
	return &checkerInjection{
		manifest: m,
		check:    check,
	}
}

type checkerInjection struct {
	manifest *Manifest
	check    func(ref ResolvedReference) error
}

func (ij *checkerInjection) Kind() string {
	return "go"
}

func (ij *checkerInjection) InjectorForInstance(inst *build.Instance, r *runtime.Runtime) (runtime.Injector, errors.Error) {
	return &checkerInjector{
		injection: ij,
		builder: refBuilder{
			inst:       inst,
			modVersion: inst.ModuleVersion.String(),
		},
		inst: inst,
	}, nil
}

type checkerInjector struct {
	builder   refBuilder
	injection *checkerInjection
	inst      *build.Instance
}

func (ij *checkerInjector) InjectedValue(attr *runtime.ExternAttr, scope *adt.Vertex) (adt.Expr, errors.Error) {
	ref, err := ij.builder.referenceForAttr(attr)
	if err != nil {
		return nil, err
	}
	resolved, rErr := ij.resolve(ref)
	if rErr != nil {
		return nil, errors.Newf(attr.Attr.Pos, "%v", rErr)
	}
	if cErr := ij.injection.check(resolved); cErr != nil {
		return nil, errors.Newf(attr.Attr.Pos, "%v", cErr)
	}
	return &adt.Top{}, nil
}

func (ij *checkerInjector) resolve(ref Reference) (ResolvedReference, error) {
	m := ij.injection.manifest
	modPath := ij.inst.ModuleVersion.Path()

	mod, ok := m.Modules[modPath]
	if !ok {
		return ResolvedReference{}, fmt.Errorf("CUE module %q not found in manifest", modPath)
	}

	cueImportPath := ij.inst.ImportPath
	inst, ok := mod.Instances[cueImportPath]
	if !ok {
		return ResolvedReference{}, fmt.Errorf("CUE instance %q not found in manifest for module %q", cueImportPath, modPath)
	}

	var goImportPath string
	if ref.GoPackage == "." {
		goImportPath = inst.ColocatedGoPackage
		if goImportPath == "" {
			return ResolvedReference{}, fmt.Errorf("no co-located Go package for CUE instance %q", cueImportPath)
		}
	} else {
		goImportPath = ref.GoPackage
	}

	if _, ok := inst.GoPackages[goImportPath]; !ok {
		return ResolvedReference{}, fmt.Errorf("Go package %q not found in manifest for CUE instance %q", goImportPath, cueImportPath)
	}

	goMod, ok := m.GoPackages[goImportPath]
	if !ok {
		return ResolvedReference{}, fmt.Errorf("Go module for package %q not found in manifest", goImportPath)
	}

	return ResolvedReference{
		CUEModule:     ij.inst.ModuleVersion,
		CUEImportPath: cueImportPath,
		GoModule:      goMod,
		GoImportPath:  goImportPath,
		Name:          ref.Name,
	}, nil
}
