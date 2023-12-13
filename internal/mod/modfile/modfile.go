// Copyright 2023 CUE Authors
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

package modfile

import (
	_ "embed"
	"fmt"
	"sync"

	"cuelang.org/go/internal/mod/semver"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/mod/module"
	"cuelang.org/go/internal/slices"
)

//go:embed schema.cue
var moduleSchemaData []byte

// File represents the contents of a cue.mod/module.cue file.
type File struct {
	Module   string          `json:"module"`
	Language *Language       `json:"language,omitempty"`
	Deps     map[string]*Dep `json:"deps,omitempty"`
	versions []module.Version
	// defaultMajorVersions maps from module base path to the
	// major version default for that path.
	defaultMajorVersions map[string]string
}

// Format returns a formatted representation of f
// in CUE syntax.
func (f *File) Format() ([]byte, error) {
	// TODO this could be better:
	// - it should omit the outer braces
	v := cuecontext.New().Encode(f)
	if err := v.Validate(cue.Concrete(true)); err != nil {
		return nil, err
	}
	n := v.Syntax(cue.Concrete(true))
	data, err := format.Node(n)
	if err != nil {
		return nil, fmt.Errorf("cannot format: %v", err)
	}
	data = append(data, '\n')
	// Sanity check that it can be parsed.
	// TODO this could be more efficient by checking all the file fields
	// before formatting the output.
	if _, err := Parse(data, "-"); err != nil {
		return nil, fmt.Errorf("cannot round-trip module file: %v", err)
	}
	return data, err
}

type Language struct {
	Version string `json:"version,omitempty"`
}

type Dep struct {
	Version string `json:"v"`
	Default bool   `json:"default,omitempty"`
}

type noDepsFile struct {
	Module string `json:"module"`
}

var (
	moduleSchemaOnce  sync.Once  // guards the creation of _moduleSchema
	moduleSchemaMutex sync.Mutex // guards any use of _moduleSchema
	_moduleSchema     cue.Value
)

func moduleSchemaDo[T any](f func(moduleSchema cue.Value) (T, error)) (T, error) {
	moduleSchemaOnce.Do(func() {
		ctx := cuecontext.New()
		schemav := ctx.CompileBytes(moduleSchemaData, cue.Filename("cuelang.org/go/internal/mod/modfile/schema.cue"))
		schemav = lookup(schemav, cue.Def("#File"))
		//schemav = schemav.Unify(lookup(schemav, cue.Hid("#Strict", "_")))
		if err := schemav.Validate(); err != nil {
			panic(fmt.Errorf("internal error: invalid CUE module.cue schema: %v", errors.Details(err, nil)))
		}
		_moduleSchema = schemav
	})
	moduleSchemaMutex.Lock()
	defer moduleSchemaMutex.Unlock()
	return f(_moduleSchema)
}

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
	return moduleSchemaDo(func(schema cue.Value) (*File, error) {
		v := schema.Context().CompileBytes(modfile, cue.Filename(filename))
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
	})
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

	mf, err := moduleSchemaDo(func(schema cue.Value) (*File, error) {
		v := schema.Context().BuildFile(file)
		if err := v.Validate(cue.Concrete(true)); err != nil {
			return nil, errors.Wrapf(err, token.NoPos, "invalid module.cue file value")
		}
		v = v.Unify(schema)
		if err := v.Validate(); err != nil {
			return nil, newCUEError(err, filename)
		}
		var mf File
		if err := v.Decode(&mf); err != nil {
			return nil, errors.Wrapf(err, token.NoPos, "internal error: cannot decode into modFile struct")
		}
		return &mf, nil
	})
	if err != nil {
		return nil, err
	}
	if strict {
		_, vers, ok := module.SplitPathVersion(mf.Module)
		if !ok {
			return nil, fmt.Errorf("module path %q in %s does not contain major version", mf.Module, filename)
		}
		if semver.Major(vers) != vers {
			return nil, fmt.Errorf("module path %s in %q should contain the major version only", mf.Module, filename)
		}
	}
	if mf.Language != nil {
		if vers := mf.Language.Version; vers != "" && !semver.IsValid(vers) {
			return nil, fmt.Errorf("language version %q in %s is not well formed", vers, filename)
		}
	}
	var versions []module.Version
	defaultMajorVersions := make(map[string]string)
	// Check that major versions match dependency versions.
	for m, dep := range mf.Deps {
		vers, err := module.NewVersion(m, dep.Version)
		if err != nil {
			return nil, fmt.Errorf("invalid module.cue file %s: cannot make version from module %q, version %q: %v", filename, m, dep.Version, err)
		}
		versions = append(versions, vers)
		if strict && vers.Path() != m {
			return nil, fmt.Errorf("invalid module.cue file %s: no major version in %q", filename, m)
		}
		if dep.Default {
			mp := vers.BasePath()
			if _, ok := defaultMajorVersions[mp]; ok {
				return nil, fmt.Errorf("multiple default major versions found for %v", mp)
			}
			defaultMajorVersions[mp] = semver.Major(vers.Version())
		}
	}

	if len(defaultMajorVersions) > 0 {
		mf.defaultMajorVersions = defaultMajorVersions
	}
	mf.versions = versions[:len(versions):len(versions)]
	module.Sort(mf.versions)
	return mf, nil
}

func newCUEError(err error, filename string) error {
	// TODO we have some potential to improve error messages here.
	return err
}

// DepVersions returns the versions of all the modules depended on by the
// file. The caller should not modify the returned slice.
//
// This always returns the same value, even if the contents
// of f are changed. If f was not created with [Parse], it returns nil.
func (f *File) DepVersions() []module.Version {
	return slices.Clip(f.versions)
}

// DefaultMajorVersions returns a map from module base path
// to the major version that's specified as the default for that module.
// The caller should not modify the returned map.
func (f *File) DefaultMajorVersions() map[string]string {
	return f.defaultMajorVersions
}
