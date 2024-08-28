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

// Package modfile provides functionality for reading and parsing
// the CUE module file, cue.mod/module.cue.
//
// WARNING: THIS PACKAGE IS EXPERIMENTAL.
// ITS API MAY CHANGE AT ANY TIME.
package modfile

import (
	_ "embed"
	"fmt"
	"slices"
	"strings"
	"sync"

	"cuelang.org/go/internal/mod/semver"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/cueversion"
	"cuelang.org/go/internal/encoding"
	"cuelang.org/go/internal/filetypes"
	"cuelang.org/go/mod/module"
)

//go:embed schema.cue
var moduleSchemaData string

const schemaFile = "cuelang.org/go/mod/modfile/schema.cue"

// File represents the contents of a cue.mod/module.cue file.
type File struct {
	// Module holds the module path, which may
	// not contain a major version suffix.
	// Use the [File.QualifiedModule] method to obtain a module
	// path that's always qualified. See also the
	// [File.ModulePath] and [File.MajorVersion] methods.
	Module   string                    `json:"module"`
	Language *Language                 `json:"language,omitempty"`
	Source   *Source                   `json:"source,omitempty"`
	Deps     map[string]*Dep           `json:"deps,omitempty"`
	Custom   map[string]map[string]any `json:"custom,omitempty"`
	versions []module.Version
	// defaultMajorVersions maps from module base path to the
	// major version default for that path.
	defaultMajorVersions map[string]string
	// actualSchemaVersion holds the actual schema version
	// that was used to validate the file. This will be one of the
	// entries in the versions field in schema.cue and
	// is set by the Parse functions.
	actualSchemaVersion string
}

// Module returns the fully qualified module path
// if is one. It returns the empty string when [ParseLegacy]
// is used and the module field is empty.
//
// Note that when the module field does not contain a major
// version suffix, "@v0" is assumed.
func (f *File) QualifiedModule() string {
	if strings.Contains(f.Module, "@") {
		return f.Module
	}
	if f.Module == "" {
		return ""
	}
	return f.Module + "@v0"
}

// ModulePath returns the path part of the module without
// its major version suffix.
func (f *File) ModulePath() string {
	path, _, _ := module.SplitPathVersion(f.QualifiedModule())
	return path
}

// MajorVersion returns the major version of the module,
// not including the "@".
// If there is no module (which can happen when [ParseLegacy]
// is used or if Module is explicitly set to an empty string),
// it returns the empty string.
func (f *File) MajorVersion() string {
	_, vers, _ := module.SplitPathVersion(f.QualifiedModule())
	return vers
}

// baseFileVersion is used to decode the language version
// to decide how to decode the rest of the file.
type baseFileVersion struct {
	Language struct {
		Version string `json:"version"`
	} `json:"language"`
}

// Source represents how to transform from a module's
// source to its actual contents.
type Source struct {
	Kind string `json:"kind"`
}

// Validate checks that src is well formed.
func (src *Source) Validate() error {
	switch src.Kind {
	case "git", "self":
		return nil
	}
	return fmt.Errorf("unrecognized source kind %q", src.Kind)
}

// Format returns a formatted representation of f
// in CUE syntax.
func (f *File) Format() ([]byte, error) {
	if len(f.Deps) == 0 && f.Deps != nil {
		// There's no way to get the CUE encoder to omit an empty
		// but non-nil slice (despite the current doc comment on
		// [cue.Context.Encode], so make a copy of f to allow us
		// to do that.
		f1 := *f
		f1.Deps = nil
		f = &f1
	}
	// TODO this could be better:
	// - it should omit the outer braces
	v := cuecontext.New().Encode(f)
	if err := v.Validate(cue.Concrete(true)); err != nil {
		return nil, err
	}
	n := v.Syntax(cue.Concrete(true)).(*ast.StructLit)

	data, err := format.Node(&ast.File{
		Decls: n.Elts,
	})
	if err != nil {
		return nil, fmt.Errorf("cannot format: %v", err)
	}
	// Sanity check that it can be parsed.
	// TODO this could be more efficient by checking all the file fields
	// before formatting the output.
	f1, err := ParseNonStrict(data, "-")
	if err != nil {
		return nil, fmt.Errorf("cannot parse result: %v", strings.TrimSuffix(errors.Details(err, nil), "\n"))
	}
	if f.Language != nil && f1.actualSchemaVersion == "v0.0.0" {
		// It's not a legacy module file (because the language field is present)
		// but we've used the legacy schema to parse it, which means that
		// it's almost certainly a bogus version because all versions
		// we care about fail when there are unknown fields, but the
		// original schema allowed all fields.
		return nil, fmt.Errorf("language version %v is too early for module.cue (need at least %v)", f.Language.Version, EarliestClosedSchemaVersion())
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
	moduleSchemaOnce sync.Once // guards the creation of _moduleSchema
	// TODO remove this mutex when https://cuelang.org/issue/2733 is fixed.
	moduleSchemaMutex sync.Mutex // guards any use of _moduleSchema
	_schemas          schemaInfo
)

type schemaInfo struct {
	Versions                    map[string]cue.Value `json:"versions"`
	EarliestClosedSchemaVersion string               `json:"earliestClosedSchemaVersion"`
}

// moduleSchemaDo runs f with information about all the schema versions
// present in schema.cue. It does this within a mutex because it is
// not currently allowed to use cue.Value concurrently.
// TODO remove the mutex when https://cuelang.org/issue/2733 is fixed.
func moduleSchemaDo[T any](f func(*schemaInfo) (T, error)) (T, error) {
	moduleSchemaOnce.Do(func() {
		// It is important that this cue.Context not be used for building any other cue.Value,
		// such as in [Parse] or [ParseLegacy].
		// A value holds memory as long as the context it was built with is kept alive for,
		// and this context is alive forever via the _schemas global.
		//
		// TODO(mvdan): this violates the documented API rules in the cue package:
		//
		//    Only values created from the same Context can be involved in the same operation.
		//
		// However, this appears to work in practice, and all alternatives right now would be
		// either too costly or awkward. We want to lift that API restriction, and this works OK,
		// so leave it as-is for the time being.
		ctx := cuecontext.New()
		schemav := ctx.CompileString(moduleSchemaData, cue.Filename(schemaFile))
		if err := schemav.Decode(&_schemas); err != nil {
			panic(fmt.Errorf("internal error: invalid CUE module.cue schema: %v", errors.Details(err, nil)))
		}
	})
	moduleSchemaMutex.Lock()
	defer moduleSchemaMutex.Unlock()
	return f(&_schemas)
}

func lookup(v cue.Value, sels ...cue.Selector) cue.Value {
	return v.LookupPath(cue.MakePath(sels...))
}

// EarliestClosedSchemaVersion returns the earliest module.cue schema version
// that excludes unknown fields. Any version declared in a module.cue file
// should be at least this, because that's when we added the language.version
// field itself.
func EarliestClosedSchemaVersion() string {
	return earliestClosedSchemaVersion()
}

var earliestClosedSchemaVersion = sync.OnceValue(func() string {
	earliest, _ := moduleSchemaDo(func(info *schemaInfo) (string, error) {
		earliest := ""
		for v := range info.Versions {
			if earliest == "" || semver.Compare(v, earliest) < 0 {
				earliest = v
			}
		}
		return earliest, nil
	})
	return earliest
})

// Parse verifies that the module file has correct syntax
// and follows the schema following the required language.version field.
// The file name is used for error messages.
// All dependencies must be specified correctly: with major
// versions in the module paths and canonical dependency versions.
func Parse(modfile []byte, filename string) (*File, error) {
	return parse(modfile, filename, true)
}

// ParseLegacy parses the legacy version of the module file
// that only supports the single field "module" and ignores all other
// fields.
func ParseLegacy(modfile []byte, filename string) (*File, error) {
	ctx := cuecontext.New()
	file, err := parseDataOnlyCUE(ctx, modfile, filename)
	if err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "invalid module.cue file syntax")
	}
	// Unfortunately we need a new context. See the note inside [moduleSchemaDo].
	v := ctx.BuildFile(file)
	if err := v.Err(); err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "invalid module.cue file")
	}
	var f noDepsFile
	if err := v.Decode(&f); err != nil {
		return nil, newCUEError(err, filename)
	}
	return &File{
		Module:              f.Module,
		actualSchemaVersion: "v0.0.0",
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

// FixLegacy converts a legacy module.cue file as parsed by [ParseLegacy]
// into a format suitable for parsing with [Parse]. It adds a language.version
// field and moves all unrecognized fields into custom.legacy.
//
// If there is no module field or it is empty, it is set to "test.example".
//
// If the file already parses OK with [ParseNonStrict], it returns the
// result of that.
func FixLegacy(modfile []byte, filename string) (*File, error) {
	f, err := ParseNonStrict(modfile, filename)
	if err == nil {
		// It parses OK so it doesn't need fixing.
		return f, nil
	}
	ctx := cuecontext.New()
	file, err := parseDataOnlyCUE(ctx, modfile, filename)
	if err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "invalid module.cue file syntax")
	}
	v := ctx.BuildFile(file)
	if err := v.Validate(cue.Concrete(true)); err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "invalid module.cue file value")
	}
	var allFields map[string]any
	if err := v.Decode(&allFields); err != nil {
		return nil, err
	}
	mpath := "test.example"
	if m, ok := allFields["module"]; ok {
		if mpath1, ok := m.(string); ok && mpath1 != "" {
			mpath = mpath1
		} else if !ok {
			return nil, fmt.Errorf("module field has unexpected type %T", m)
		}
		// TODO decide what to do if the module path isn't OK according to the new rules.
	}
	customLegacy := make(map[string]any)
	for k, v := range allFields {
		if k != "module" {
			customLegacy[k] = v
		}
	}
	var custom map[string]map[string]any
	if len(customLegacy) > 0 {
		custom = map[string]map[string]any{
			"legacy": customLegacy,
		}
	}
	f = &File{
		Module: mpath,
		Language: &Language{
			// If there's a legacy module file, the CUE code
			// is unlikely to be using new language features,
			// so keep the language version fixed rather than
			// using [cueversion.LanguageVersion].
			// See https://cuelang.org/issue/3222.
			Version: "v0.9.0",
		},
		Custom: custom,
	}
	// Round-trip through [Parse] so that we get exactly the same
	// result as a later parse of the same data will. This also
	// adds a major version to the module path if needed.
	data, err := f.Format()
	if err != nil {
		return nil, fmt.Errorf("cannot format fixed file: %v", err)
	}
	f, err = ParseNonStrict(data, "fixed-"+filename)
	if err != nil {
		return nil, fmt.Errorf("cannot parse resulting module file %q: %v", data, err)
	}
	return f, nil
}

func parse(modfile []byte, filename string, strict bool) (*File, error) {
	// Unfortunately we need a new context. See the note inside [moduleSchemaDo].
	ctx := cuecontext.New()
	file, err := parseDataOnlyCUE(ctx, modfile, filename)
	if err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "invalid module.cue file syntax")
	}

	v := ctx.BuildFile(file)
	if err := v.Validate(cue.Concrete(true)); err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "invalid module.cue file value")
	}
	// First determine the declared version of the module file.
	var base baseFileVersion
	if err := v.Decode(&base); err != nil {
		return nil, errors.Wrapf(err, token.NoPos, "cannot determine language version")
	}
	if base.Language.Version == "" {
		return nil, ErrNoLanguageVersion
	}
	if !semver.IsValid(base.Language.Version) {
		return nil, fmt.Errorf("language version %q in module.cue is not valid semantic version", base.Language.Version)
	}
	if mv, lv := base.Language.Version, cueversion.LanguageVersion(); semver.Compare(mv, lv) > 0 {
		return nil, fmt.Errorf("language version %q declared in module.cue is too new for current language version %q", mv, lv)
	}

	mf, err := moduleSchemaDo(func(schemas *schemaInfo) (*File, error) {
		// Now that we're happy we're within bounds, find the latest
		// schema that applies to the declared version.
		latest := ""
		var latestSchema cue.Value
		for vers, schema := range schemas.Versions {
			if semver.Compare(vers, base.Language.Version) > 0 {
				continue
			}
			if latest == "" || semver.Compare(vers, latest) > 0 {
				latest = vers
				latestSchema = schema
			}
		}
		if latest == "" {
			// Should never happen, because there should always
			// be some applicable schema.
			return nil, fmt.Errorf("cannot find schema suitable for reading module file with language version %q", base.Language.Version)
		}
		schema := latestSchema
		v = v.Unify(lookup(schema, cue.Def("#File")))
		if err := v.Validate(); err != nil {
			return nil, newCUEError(err, filename)
		}
		if latest == "v0.0.0" {
			// The chosen schema is the earliest schema which allowed
			// all fields. We don't actually want a module.cue file with
			// an old version to treat those fields as special, so don't try
			// to decode into *File because that will do so.
			// This mirrors the behavior of [ParseLegacy].
			var f noDepsFile
			if err := v.Decode(&f); err != nil {
				return nil, newCUEError(err, filename)
			}
			return &File{
				Module:              f.Module,
				actualSchemaVersion: "v0.0.0",
			}, nil
		}
		var mf File
		if err := v.Decode(&mf); err != nil {
			return nil, errors.Wrapf(err, token.NoPos, "internal error: cannot decode into modFile struct")
		}
		mf.actualSchemaVersion = latest
		return &mf, nil
	})
	if err != nil {
		return nil, err
	}
	mainPath, mainMajor, ok := module.SplitPathVersion(mf.Module)
	if ok {
		if semver.Major(mainMajor) != mainMajor {
			return nil, fmt.Errorf("module path %s in %q should contain the major version only", mf.Module, filename)
		}
	} else if mainPath = mf.Module; mainPath != "" {
		if err := module.CheckPathWithoutVersion(mainPath); err != nil {
			return nil, fmt.Errorf("module path %q in %q is not valid: %v", mainPath, filename, err)
		}
		// There's no main module major version: default to v0.
		mainMajor = "v0"
	}
	if mf.Language != nil {
		vers := mf.Language.Version
		if !semver.IsValid(vers) {
			return nil, fmt.Errorf("language version %q in %s is not well formed", vers, filename)
		}
		if semver.Canonical(vers) != vers {
			return nil, fmt.Errorf("language version %v in %s is not canonical", vers, filename)
		}
	}
	var versions []module.Version
	// The main module is always the default for its own major version.
	defaultMajorVersions := map[string]string{
		mainPath: mainMajor,
	}
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

// ErrNoLanguageVersion is returned by [Parse] and [ParseNonStrict]
// when a cue.mod/module.cue file lacks the `language.version` field.
var ErrNoLanguageVersion = fmt.Errorf("no language version declared in module.cue")

func parseDataOnlyCUE(ctx *cue.Context, cueData []byte, filename string) (*ast.File, error) {
	dec := encoding.NewDecoder(ctx, &build.File{
		Filename:       filename,
		Encoding:       build.CUE,
		Interpretation: build.Auto,
		Form:           build.Data,
		Source:         cueData,
	}, &encoding.Config{
		Mode:      filetypes.Export,
		AllErrors: true,
	})
	if err := dec.Err(); err != nil {
		return nil, err
	}
	return dec.File(), nil
}

func newCUEError(err error, filename string) error {
	ps := errors.Positions(err)
	for _, p := range ps {
		if errStr := findErrorComment(p); errStr != "" {
			return fmt.Errorf("invalid module.cue file: %s", errStr)
		}
	}
	// TODO we have more potential to improve error messages here.
	return err
}

// findErrorComment finds an error comment in the form
//
//	//error: ...
//
// before the given position.
// This works as a kind of poor-man's error primitive
// so we can customize the error strings when verification
// fails.
func findErrorComment(p token.Pos) string {
	if p.Filename() != schemaFile {
		return ""
	}
	off := p.Offset()
	source := moduleSchemaData
	if off > len(source) {
		return ""
	}
	source, _, ok := cutLast(source[:off], "\n")
	if !ok {
		return ""
	}
	_, errorLine, ok := cutLast(source, "\n")
	if !ok {
		return ""
	}
	errStr, ok := strings.CutPrefix(errorLine, "//error: ")
	if !ok {
		return ""
	}
	return errStr
}

func cutLast(s, sep string) (before, after string, found bool) {
	if i := strings.LastIndex(s, sep); i >= 0 {
		return s[:i], s[i+len(sep):], true
	}
	return "", s, false
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
