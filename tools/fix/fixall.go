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
func Instances(insts []*build.Instance, o ...Option) errors.Error {
	// Parse options to check for upgrade
	opts := options{}
	for _, fn := range o {
		fn(&opts)
	}

	done := map[*ast.File]bool{}

	for _, b := range insts {
		var version string

		if b.ModuleFile != nil && b.ModuleFile.Language != nil {
			version = b.ModuleFile.Language.Version
		}

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

		for _, f := range b.Files {
			if done[f] {
				continue
			}
			done[f] = true
			_, err := file(f, version, o...)
			if err != nil {
				return err
			}
		}

		if err := astutil.SanitizeFiles(b.Files); err != nil {
			return errors.Wrapf(err, token.NoPos, "fix: sanitize failed")
		}
	}
	return nil
}
