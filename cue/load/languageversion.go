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

package load

import (
	"cmp"
	"path"
	"slices"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/cueversion"
	"cuelang.org/go/internal/filetypes"
	"cuelang.org/go/internal/mod/langversion"
	pkgpath "cuelang.org/go/pkg/path"
)

// completeFor completes c for loading args. [Config.LanguageVersion] is a
// fallback for input that belongs to no module: if no argument belongs to a
// module, it returns a configuration for a load outside any module at that
// version; otherwise it returns the configuration of the load without it,
// in which only the files belonging to no module fall back to it. Either
// way, the fallback is never an error, even if it applies to no input.
//
// No module file is read before the arguments are classified, so that a
// broken module which no argument belongs to cannot fail the load.
func (c Config) completeFor(args []string) (*Config, error) {
	v := c.LanguageVersion
	if v != "" {
		var err error
		if v, err = cueversion.CheckLanguageVersion(v); err != nil {
			return nil, err
		}
	}
	c.LanguageVersion = v
	cfg, err := c.complete()
	if err != nil || v == "" {
		return cfg, err
	}
	normalized, err := cfg.normalizeArgs(args)
	if err != nil {
		return nil, err
	}
	if !cfg.anyInModule(normalized) {
		return cfg, nil
	}
	c.LanguageVersion = ""
	if cfg, err = c.complete(); err != nil {
		return nil, err
	}
	cfg.fallbackVersion = v
	cfg.fallbackFiles = map[*build.File]bool{}
	return cfg, nil
}

// anyInModule reports whether any of args, normalized by
// [Config.normalizeArgs], belongs to a module: a package named by import
// path, or a package directory or file within a module.
func (c *Config) anyInModule(args []string) bool {
	pkgArgs, otherArgs := filetypes.SplitArgs(args)
	files, _ := filetypes.ParseArgs(otherArgs) // errors are reported by Instances
	for _, arg := range pkgArgs {
		ip := ast.ParseImportPath(arg)
		if !isLocalImport(ip.Path) {
			return true
		}
		dir := ip.Path
		if before, _, ok := strings.Cut(dir, "..."); ok {
			// The directory a pattern's scan starts at, as in
			// [loader.matchPackagesInFS]; the scan itself skips nested
			// modules.
			dir, _ = path.Split(before)
		}
		if c.inModule(c.absPath(cmp.Or(strings.TrimSuffix(dir, "/"), "."))) {
			return true
		}
	}
	for _, f := range files {
		// Standard input belongs to no module.
		if f.Filename != "-" && c.inModule(pkgpath.Dir(c.absPath(f.Filename), c.pathOS)) {
			return true
		}
	}
	return false
}

// inModule reports whether dir lies within a module: walking up from it, as
// [Config.findModRoot] does, finds a cue.mod directory with a module file. A
// legacy cue.mod file, or a cue.mod directory without a module file, does
// not make a module.
func (c *Config) inModule(dir string) bool {
	root := c.findModRoot(dir)
	if root == "" {
		return false
	}
	_, err := c.fileSystem.stat(pkgpath.Join([]string{root, modDir, moduleFile}, c.pathOS))
	return err == nil
}

// normalizeArgs rewrites absolute directory arguments as relative paths
// rooted at [Config.Dir] so the rest of the loading logic handles them as
// ordinary local import paths. This must happen before the package/file
// split, as [filetypes.IsPackage] rejects Windows paths like `C:\foo` due to
// the colon. It also rewrites \ to / as a courtesy to Windows developers.
func (c *Config) normalizeArgs(args []string) ([]string, error) {
	args = slices.Clone(args)
	for i, p := range args {
		if pkgpath.IsAbs(p, c.pathOS) {
			rel, err := pkgpath.Rel(c.Dir, p, c.pathOS)
			if err != nil {
				return nil, err
			}
			p = "./" + rel
		}
		args[i] = pkgpath.ToSlash(p, c.pathOS)
	}
	return args, nil
}

// absPath returns the absolute form of a slash-separated path argument.
func (c *Config) absPath(name string) string {
	name = pkgpath.FromSlash(name, c.pathOS)
	if pkgpath.IsAbs(name, c.pathOS) {
		return pkgpath.Clean(name, c.pathOS)
	}
	return pkgpath.Join([]string{c.Dir, name}, c.pathOS)
}

// checkVersionAttr reports an error if f, the file at path, carries a
// @version attribute while it belongs to a module that declares a language
// version, as the module file decides the version of its files. As for
// [Config.LanguageVersion], a file belongs to the module found by walking up
// from its location, whichever load it is part of.
func (c *Config) checkVersionAttr(path string, f *ast.File) errors.Error {
	a := internal.VersionAttr(f)
	if a == nil || path == "-" {
		return nil
	}
	root := c.findModRoot(pkgpath.Dir(path, c.pathOS))
	if root == "" {
		return nil
	}
	mf := c.modFile
	if c.modFile == nil || root != c.ModuleRoot {
		mf = c.replaceDirModFile(root)
	}
	if mf == nil || mf.Language == nil || mf.Language.Version == "" {
		return nil
	}
	modFile := pkgpath.Join([]string{root, modDir, moduleFile}, c.pathOS)
	if rel, err := pkgpath.Rel(c.Dir, modFile, c.pathOS); err == nil && !strings.HasPrefix(rel, "..") {
		modFile = pkgpath.ToSlash(rel, c.pathOS)
	}
	return langversion.AttrInModuleError(a.Pos(), modFile, mf.Language.Version)
}
