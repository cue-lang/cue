package modfile

import (
	_ "embed"
	"fmt"
	"strings"

	"cuelang.org/go/internal/mod/semver"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/mod/module"
)

//go:embed schema.cue
var moduleSchemaData []byte

type File struct {
	Module   string          `json:"module"`
	Language Language        `json:"language"`
	Deps     map[string]*Dep `json:"deps,omitempty"`
	versions []module.Version
}

type Language struct {
	Version string `json:"version"`
}

type Dep struct {
	Version string `json:"v"`
	Default bool   `json:"default,omitempty"`
}

type noDepsFile struct {
	Module string `json:"module"`
}

var moduleSchema = func() cue.Value {
	ctx := cuecontext.New()
	schemav := ctx.CompileBytes(moduleSchemaData, cue.Filename("cuelang.org/go/internal/mod/modfile/schema.cue"))
	//schemav = schemav.Unify(lookup(schemav, cue.Hid("_#Strict", "_")))
	//schemav = schemav.Unify(lookup(schemav, cue.Hid("_#Reserved", "_")))
	// TODO Use schemav.Validate when it doesn't give an error for required fields.
	if err := schemav.Err(); err != nil {
		panic(fmt.Errorf("internal error: invalid CUE module.cue schema: %v", errors.Details(err, nil)))
	}

	return schemav
}()

func lookup(v cue.Value, sels ...cue.Selector) cue.Value {
	return v.LookupPath(cue.MakePath(sels...))
}

// Parse verifies that the module file has correct syntax.
// The file name is used for error messages.
// All dependencies must be specified correctly: with major
// versions in the module paths and canonical dependency
// versions.
func Parse(modfile []byte, filename string) (*File, error) {
	return parse(modfile, filename, true)
}

// ParseLegacy parses the legacy version of the module file
// that only supports the single field "module" and ignores all other
// fields.
func ParseLegacy(modfile []byte, filename string) (*File, error) {
	v := moduleSchema.Context().CompileBytes(modfile, cue.Filename(filename))
	if err := v.Err(); err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "invalid module.cue file")
	}
	var f noDepsFile
	if err := v.Decode(&f); err != nil {
		return nil, newCUEError(err, filename)
	}
	return &File{
		Module: f.Module,
	}, nil
}

// ParseNonStrict is like Parse but allows some laxity in the parsing:
//   - if a module path lacks a version, it's taken from the version.
//   - if a non-canonical version is used, it will be canonicalized.
//
// The file name is used for error messages.
func ParseNonStrict(modfile []byte, filename string) (*File, error) {
	return parse(modfile, filename, false)
}

func parse(modfile []byte, filename string, strict bool) (*File, error) {
	file, err := parser.ParseFile(filename, modfile)
	if err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "invalid module.cue file syntax")
	}
	// TODO disallow non-data-mode CUE.

	v := moduleSchema.Context().BuildFile(file)
	if err := v.Validate(cue.Concrete(true)); err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "invalid module.cue file value")
	}
	v = v.Unify(moduleSchema)
	if err := v.Validate(); err != nil {
		return nil, newCUEError(err, filename)
	}
	var mf File
	if err := v.Decode(&mf); err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "internal error: cannot decode into modFile struct")
	}
	if strict {
		_, v, ok := module.SplitPathVersion(mf.Module)
		if !ok {
			return nil, fmt.Errorf("module path %q in %s does not contain major version", mf.Module, filename)
		}
		if semver.Major(v) != v {
			return nil, fmt.Errorf("module path %s in %q should contain the major version only", mf.Module, filename)
		}
	}
	var versions []module.Version
	// Check that major versions match dependency versions.
	for m, dep := range mf.Deps {
		v, err := module.NewVersion(m, dep.Version)
		if err != nil {
			return nil, fmt.Errorf("invalid module.cue file %s: cannot make version from module %q, version %q: %v", filename, m, dep.Version, err)
		}
		versions = append(versions, v)
		if strict && v.Path() != m {
			return nil, fmt.Errorf("invalid module.cue file %s: no major version in %q", filename, m)
		}
	}

	mf.versions = versions[:len(versions):len(versions)]
	return &mf, nil
}

// The default CUE errors are not great currently, particularly when the recipient doesn't
// necessarily have access to the original schema, so just include a list of
// all the fields where there are errors.
func newCUEError(err error, filename string) error {
	if paths := badPaths(err); len(paths) > 0 {
		f := "fields"
		if len(paths) == 1 {
			f = "field"
		}
		return fmt.Errorf("invalid module.cue file %s: errors in the following %s: %s", filename, f, strings.Join(paths, ", "))
	}
	return fmt.Errorf("%v", errors.Details(err, nil)) // errors.Wrapf(err, token.NoPos, "invalid module.cue file")
}

func (f *File) Versions() []module.Version {
	return f.versions
}

func badPaths(err error) []string {
	paths := make(map[string]bool)
	errs := errors.Errors(err)
	for _, err := range errs {
		if p := err.Path(); len(p) > 0 {
			paths[strings.Join(p, ".")] = true
		}
	}
	all := make([]string, 0, len(paths))
	for p := range paths {
		all = append(all, p)
	}
	return all
}
