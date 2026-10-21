// Copyright 2020 CUE Authors
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

package fix

import (
	"path/filepath"
	"slices"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/mod/modfile"
)

// Instances modifies all files contained in the given build instances at once.
//
// It also applies the fixes from [File].
//
// A file that cannot be rewritten or sanitized does not stop the others:
// every such failure is reported together, positioned in the file it
// concerns, once all instances have been processed. A failure that applies
// to a whole instance, such as an experiment that cannot apply at its
// language version, is reported at once.
func Instances(insts []*build.Instance, o ...Option) errors.Error {
	// Parse options to check for upgrade
	opts := options{}
	for _, fn := range o {
		fn(&opts)
	}

	done := map[*ast.File]bool{}
	var errs errors.Error

	for _, b := range insts {
		version := b.LanguageVersion()

		// Update module file language version if upgrading
		if opts.upgradeVersion != "" && b.ModuleFile != nil &&
			(b.ModuleFile.Language == nil || b.ModuleFile.Language.Version != opts.upgradeVersion) {
			// Update the language version in memory
			if b.ModuleFile.Language == nil {
				b.ModuleFile.Language = &modfile.Language{}
			}
			b.ModuleFile.Language.Version = opts.upgradeVersion

			// Re-initialize to validate
			if err := b.ModuleFile.Init(); err != nil {
				return errors.Wrapf(err, token.NoPos, "fix: failed to validate updated module file")
			}
		}

		// A file that declares its own version with a @version attribute is
		// judged at that version rather than the instance's.
		ownVersion := func(f *ast.File) bool { return internal.VersionAttr(f) != nil }

		// Validate the requested experiments against the instance's
		// language version, so that an instance whose files do not even
		// parse at that version, and so has no files, reports them too.
		if version != "" && !slices.ContainsFunc(b.Files, ownVersion) {
			if _, _, err := opts.resolveExperiments(version); err != nil {
				return err
			}
		}

		for _, f := range b.Files {
			if done[f] {
				continue
			}
			done[f] = true
			fileVersion := version
			if ownVersion(f) {
				fileVersion = f.Pos().LanguageVersion()
			}
			if _, err := file(f, fileVersion, outsideModule(b, f), o...); err != nil {
				errs = errors.Append(errs, err)
			}
		}

		// Sanitizing sees every file of the package, so that a name a
		// rewrite inserted is renamed where a sibling file declares it.
		if err := astutil.SanitizeFiles(b.Files); err != nil {
			errs = errors.Append(errs, errors.Promote(err, "fix: sanitize failed"))
		}
	}
	return errs
}

// outsideModule reports whether f, a file of b, belongs to no module: b has
// no module, or f lies outside its root, as a file named on the command line
// may while still joining the load of the module holding the working
// directory. Files under cue.mod, and those of an instance that knows no
// root, belong to the module, whose version they are fixed and read at.
func outsideModule(b *build.Instance, f *ast.File) bool {
	if b.ModuleFile == nil {
		return true
	}
	if b.Root == "" {
		return false
	}
	rel, err := filepath.Rel(b.Root, f.Filename)
	if err != nil {
		return false
	}
	first, _, _ := strings.Cut(filepath.ToSlash(rel), "/")
	return first == ".."
}
