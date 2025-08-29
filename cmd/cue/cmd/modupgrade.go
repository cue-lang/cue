// Copyright 2025 The CUE Authors
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

package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/mod/semver"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/tools/fix"
	"github.com/spf13/cobra"
)

func newModUpgradeCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "upgrade <version>",
		Short: "upgrade the current module to a new language version",
		Long: `The version should be a valid semver version. It will update
the language version in the module file, and apply fixes to the CUE
code to accommodate backward compatibility.
`,
		RunE: mkRunE(c, runModUpgrade),
		Args: cobra.ExactArgs(1),

		// TODO(upgrade): hide until we have a case where an experimental
		// version is accepted. See other TODO(upgrade) in this file.
		Hidden: true,
	}

	return cmd
}

func runModUpgrade(cmd *Command, args []string) error {
	if !semver.IsValid(args[0]) {
		return fmt.Errorf("invalid version %q; must be valid semantic version (see http://semver.org)", args[0])
	}

	var opts []fix.Option

	// TODO(upgrade): this is just for testing. Remove this line and update the
	// failing test to a later version.when unhiding this command.
	opts = append(opts, fix.Experiments("explicitopen"))

	opts = append(opts, fix.UpgradeVersion(args[0]))

	instances, errs := fixInstances(cmd, nil, false, opts...)
	if errs != nil {
		return errs
	}

	// Write updated module files to disk if upgrade was requested
	for _, i := range instances {
		if i.ModuleFile != nil && i.Root != "" {
			// Format and write the module file
			data, err := modfile.Format(i.ModuleFile)
			if err != nil {
				errs = errors.Append(errs, errors.Wrapf(err, token.NoPos, "failed to format module file"))
				continue
			}

			moduleFilePath := filepath.Join(i.Root, "cue.mod", "module.cue")
			if err := os.WriteFile(moduleFilePath, data, 0666); err != nil {
				errs = errors.Append(errs, errors.Wrapf(err, token.NoPos, "failed to write module file"))
			}
		}
	}

	return errs
}
