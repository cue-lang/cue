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
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
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

		// Validate the requested experiments against the instance's
		// language version, so that an instance whose files do not even
		// parse at that version, and so has no files, reports them too.
		if version != "" {
			if _, _, err := opts.resolveExperiments(version); err != nil {
				return err
			}
		}

		for _, f := range b.Files {
			if done[f] {
				continue
			}
			done[f] = true
			if _, err := file(f, version, o...); err != nil {
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
