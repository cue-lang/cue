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

package modpkgload

import (
	"context"
	"fmt"
	"io/fs"
	"iter"
	"maps"
	"path"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal/mod/semver"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/module"
	cuepath "cuelang.org/go/pkg/path"
)

// FullRegistry is a [Registry] that can also report module files and the
// available versions of a module. It is the set of registry operations
// needed to serve replaced modules.
type FullRegistry interface {
	Registry

	// ModFile returns the module file for the given module version.
	// The caller must not mutate the returned value.
	ModFile(ctx context.Context, mv module.Version) (*modfile.File, error)

	// ModuleVersions returns all the versions for the module with the given
	// path, sorted in semver order.
	ModuleVersions(ctx context.Context, mpath string) ([]string, error)
}

// NewReplacingRegistry returns a registry that serves replaced modules from
// their replacement sources (a local directory or a different module
// version) and delegates everything else to reg. If repls is nil, reg is
// returned unchanged.
//
// locForPath returns the source location corresponding to a replacement directory
// path in repls, relative to the given absolute directory relTo if it's not absolute.
// If it's nil, directory replace directives are not supported.
func NewReplacingRegistry(
	reg FullRegistry,
	repls *Replacements,
	locForPath func(path string) (module.SourceLoc, error),
) FullRegistry {
	if repls == nil {
		return reg
	}
	if locForPath == nil {
		locForPath = func(path string) (module.SourceLoc, error) {
			return module.SourceLoc{}, fmt.Errorf("directory replace directives not supported")
		}
	}
	return &replacingRegistry{
		underlying: reg,
		repls:      repls,
		locForPath: locForPath,
	}
}

// replacingRegistry wraps a [FullRegistry] so that replaced modules are
// served from their replacement sources.
type replacingRegistry struct {
	underlying FullRegistry
	repls      *Replacements
	locForPath func(path string) (module.SourceLoc, error)
}

func (r *replacingRegistry) replacement(m module.Version) (Replacement, bool) {
	return r.repls.Lookup(m.Path())
}

func (r *replacingRegistry) Fetch(ctx context.Context, m module.Version) (module.SourceLoc, error) {
	repl, ok := r.replacement(m)
	if !ok {
		return r.underlying.Fetch(ctx, m)
	}
	if repl.Dir != "" {
		return r.locForPath(repl.Dir)
	}
	return r.underlying.Fetch(ctx, repl.Module)
}

func (r *replacingRegistry) ModFile(ctx context.Context, mv module.Version) (*modfile.File, error) {
	repl, ok := r.replacement(mv)
	if !ok {
		return r.underlying.ModFile(ctx, mv)
	}
	if repl.Dir != "" {
		return r.modFileFromDir(repl.Dir)
	}
	return r.underlying.ModFile(ctx, repl.Module)
}

func (r *replacingRegistry) ModuleVersions(ctx context.Context, mpath string) ([]string, error) {
	return r.underlying.ModuleVersions(ctx, mpath)
}

func (r *replacingRegistry) modFileFromDir(dir string) (*modfile.File, error) {
	loc, err := r.locForPath(dir)
	if err != nil {
		return nil, fmt.Errorf("cannot get location for replacement directory: %v", err)
	}
	// TODO the fs.FS paths below might not have any relevance to the
	// user when presented in an error: perhaps we should include the
	// underlying OS path in the error if available?
	data, err := fs.ReadFile(loc.FS, path.Join(loc.Dir, "cue.mod/module.cue"))
	if err != nil {
		return nil, fmt.Errorf("cannot read module file in replacement directory: %v", err)
	}
	mf, err := modfile.ParseNonStrict(data, path.Join(dir, "cue.mod/module.cue"))
	if err != nil {
		return nil, fmt.Errorf("cannot parse module file in replacement directory: %v", err)
	}
	return mf, nil
}

// Replacement describes a single replacement directive.
type Replacement struct {
	// Module holds the replacement module version.
	// It is non-zero for module-version replacements.
	Module module.Version

	// Dir holds the replacement directory path.
	// It is non-empty for directory replacements.
	Dir string
}

// Replacements holds replacement directives and provides both forward
// lookups (original module path → replacement) and reverse lookups
// (replacement import path → canonical import path under the original module).
type Replacements struct {
	// forward maps from original module path (including major version) to
	// its replacement. Keying by the full path makes a replacement specific
	// to a single major version.
	forward map[string]Replacement

	// reverse maps from replacement module base path to original module base path.
	// Only populated for module-version replacements (not directory replacements).
	reverse map[string]string
}

// NewReplacements builds the replacements described by the deps of a
// module file, keyed by original module base path. It returns nil if there
// are no replace directives.
//
// A module-version replace directive names only the major version of the
// replacement target (for example "example.com/bar@v0"); the concrete
// version is taken from the target's own dependency entry in mf, so the
// replacement is subject to the same minimum-version selection as any other
// dependency. The target must therefore be listed as a dependency with a
// version. A bare module path (no major version) is also accepted when mf
// records a default major version for it.
func NewReplacements(mf *modfile.File) (*Replacements, error) {
	r := &Replacements{
		forward: make(map[string]Replacement),
		reverse: make(map[string]string),
	}
	for mpath, dep := range mf.Deps {
		if dep.Replace == "" {
			continue
		}
		mv, err := module.NewVersion(mpath, dep.Version)
		if err != nil {
			return nil, fmt.Errorf("cannot make version from module %q, version %q: %v", mpath, dep.Version, err)
		}
		repl, err := resolveReplacement(dep.Replace, mf)
		if err != nil {
			return nil, fmt.Errorf("invalid replace value for %s: %v", mpath, err)
		}
		r.forward[mv.Path()] = repl
		if repl.Module.IsValid() {
			r.reverse[repl.Module.BasePath()] = mv.BasePath()
		}
	}
	if len(r.forward) == 0 {
		return nil, nil
	}
	return r, nil
}

// resolveReplacement turns a replace directive value into a Replacement,
// resolving a module-version target to a concrete version. The version is
// taken from the target's own dependency entry in mf (reflecting
// minimum-version selection); if the target is not listed as a dependency but
// the directive named a full version, that version is used. Directory
// replacements are returned unchanged.
func resolveReplacement(s string, mf *modfile.File) (Replacement, error) {
	if isReplaceDirectoryPath(s) {
		return Replacement{Dir: s}, nil
	}
	base, major, full, err := parseReplaceModule(s)
	if err != nil {
		return Replacement{}, err
	}
	if major == "" {
		major = mf.DefaultMajorVersions()[base]
		if major == "" {
			return Replacement{}, fmt.Errorf("replacement %q has no major version and no default major version is set for it", s)
		}
	}
	targetPath := base + "@" + major
	version := full
	if dep, ok := mf.Deps[targetPath]; ok && dep.Version != "" {
		// The target's dependency entry reflects minimum-version selection,
		// so prefer it over any version named in the directive itself.
		version = dep.Version
	}
	if version == "" {
		return Replacement{}, fmt.Errorf("replacement target %q must be listed as a dependency with a version", targetPath)
	}
	mv, err := module.NewVersion(targetPath, version)
	if err != nil {
		return Replacement{}, err
	}
	return Replacement{Module: mv}, nil
}

// All returns all the replacements as (modulePath, replacement) pairs
// in arbitrary order.
func (r *Replacements) All() iter.Seq2[string, Replacement] {
	return maps.All(r.forward)
}

// Lookup returns the replacement for the given module path (including its
// major version suffix, e.g. "example.com/foo@v1"), or ok=false if there is
// no replacement. Replacements are specific to a major version: a
// replacement for "example.com/foo@v1" does not apply to
// "example.com/foo@v2".
func (r *Replacements) Lookup(modulePath string) (Replacement, bool) {
	if r == nil {
		return Replacement{}, false
	}
	repl, ok := r.forward[modulePath]
	return repl, ok
}

// CanonicalImportPath rewrites importPath if it falls under a replacement
// module's namespace. For example, if original module "a.com/foo" is replaced
// by "b.com/bar", then "b.com/bar/subpkg" is rewritten to "a.com/foo/subpkg".
//
// If no rewriting is needed, importPath is returned unchanged.
func (r *Replacements) CanonicalImportPath(importPath string) string {
	if r == nil {
		return importPath
	}
	// Parse the import path so we can match against the bare path
	// without the version suffix (the reverse map stores base paths
	// without major version suffixes).
	parts := ast.ParseImportPath(importPath)
	for p := parts.Path; ; {
		if origBase, ok := r.reverse[p]; ok {
			if len(parts.Path) > len(p) {
				parts.Path = origBase + parts.Path[len(p):]
			} else {
				parts.Path = origBase
			}
			return parts.String()
		}
		i := strings.LastIndex(p, "/")
		if i < 0 {
			break
		}
		p = p[:i]
	}
	return importPath
}

// ParseReplacement checks the syntax of a replace directive value. The value
// is either a directory path (starting with ".", "/" or a Windows drive
// letter) or a module path. The module path may be bare ("example.com/bar"),
// carry a major version ("example.com/bar@v0") or carry a full version
// ("example.com/bar@v0.1.0"); a full version is accepted but is normalized to
// a bare major version by cue mod tidy. For a value that names a major
// version it returns a Replacement holding the version-less target module.
func ParseReplacement(s string) (Replacement, error) {
	if isReplaceDirectoryPath(s) {
		return Replacement{Dir: s}, nil
	}
	base, major, _, err := parseReplaceModule(s)
	if err != nil {
		return Replacement{}, err
	}
	if major == "" {
		// Bare path: valid syntactically; the major version is resolved later.
		return Replacement{}, nil
	}
	mv, err := module.NewVersion(base+"@"+major, "")
	if err != nil {
		return Replacement{}, err
	}
	return Replacement{Module: mv}, nil
}

// ReplaceTarget reports the module target named by a replace directive value.
// For a module-version replacement it returns the target's base path, its
// major version (empty when the value omitted it, e.g. a bare module path) and
// the full version it named (empty if it named only a major version); ok is
// true. For a directory replacement ok is false.
func ReplaceTarget(s string) (base, major, full string, ok bool, err error) {
	if isReplaceDirectoryPath(s) {
		return "", "", "", false, nil
	}
	base, major, full, err = parseReplaceModule(s)
	if err != nil {
		return "", "", "", false, err
	}
	return base, major, full, true, nil
}

// NormalizeReplace returns the canonical form of a replace directive value: a
// directory path is returned unchanged, a module-version target that names a
// full version is reduced to its bare major version (e.g.
// "example.com/bar@v0.1.0" becomes "example.com/bar@v0"), and a value that
// already names only a major version (or a bare path) is returned unchanged.
func NormalizeReplace(s string) (string, error) {
	if isReplaceDirectoryPath(s) {
		return s, nil
	}
	base, major, full, err := parseReplaceModule(s)
	if err != nil {
		return "", err
	}
	if full != "" {
		return base + "@" + major, nil
	}
	return s, nil
}

// parseReplaceModule parses the module form of a replace directive value into
// the target's base path, its major version (if present) and its full version
// (if the value named one). A value may be a bare module path
// ("example.com/bar"), a path with a major version ("example.com/bar@v0"), or
// a path with a full version ("example.com/bar@v0.1.0"). A full version is
// accepted for convenience but is normalized to a bare major version by cue
// mod tidy (see [NormalizeReplace]); the concrete version of the target is
// taken from its dependency entry, subject to minimum-version selection.
func parseReplaceModule(s string) (base, major, full string, err error) {
	base, vers, ok := ast.SplitPackageVersion(s)
	if !ok {
		// No "@version" suffix: a bare module path whose major version is
		// resolved from the default major version.
		if err := module.CheckPathWithoutVersion(s); err != nil {
			return "", "", "", fmt.Errorf("invalid replacement module path %q: %v", s, err)
		}
		return s, "", "", nil
	}
	if isMajorVersion(vers) {
		return base, vers, "", nil
	}
	if !semver.IsValid(vers) || semver.Canonical(vers) != vers {
		return "", "", "", fmt.Errorf("invalid version %q in replacement %q", vers, s)
	}
	return base, semver.Major(vers), vers, nil
}

// isMajorVersion reports whether v is a bare major version such as "v0" or
// "v12" (as opposed to a full semantic version like "v0.1.0").
func isMajorVersion(v string) bool {
	if len(v) < 2 || v[0] != 'v' {
		return false
	}
	for _, c := range v[1:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// isReplaceDirectoryPath reports whether the given string looks like a
// filesystem path (as opposed to a module path with version).
// A value is a directory path if it starts with ".", "/" or matches a
// Windows absolute path (drive letter followed by colon).
func isReplaceDirectoryPath(s string) bool {
	if strings.HasPrefix(s, ".") || strings.HasPrefix(s, "/") {
		return true
	}
	// A leading volume name (e.g. a Windows drive letter "C:" or a UNC
	// path "\\host\share") indicates an absolute filesystem path.
	return cuepath.VolumeName(s, cuepath.Windows) != ""
}
