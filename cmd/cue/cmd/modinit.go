// Copyright 2019 CUE Authors
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
	"strings"

	"github.com/spf13/cobra"

	"cuelang.org/go/internal/cueexperiment"
	"cuelang.org/go/internal/cueversion"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/module"
)

func newModInitCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init [module]",
		Short: "initialize new module in current directory",
		Long: `Init initializes a cue.mod directory in the current directory, in effect
creating a new module rooted at the current directory. The cue.mod
directory must not already exist. A legacy cue.mod file in the current
directory is moved to the new subdirectory.

A module name is optional, but if it is not given, a package
within the module cannot import another package defined
in the module.
`,
		RunE: mkRunE(c, runModInit),
	}

	cmd.Flags().BoolP(string(flagForce), "f", false, "force moving old-style cue.mod file")
	cmd.Flags().String(string(flagSource), "", "set the source field")
	return cmd
}

func runModInit(cmd *Command, args []string) (err error) {
	modulePath := ""
	if len(args) > 0 {
		if len(args) != 1 {
			return fmt.Errorf("too many arguments")
		}
		modulePath = args[0]
		if err := module.CheckPath(modulePath); err != nil {
			// It might just be lacking a major version.
			if err1 := module.CheckPathWithoutVersion(modulePath); err1 != nil {
				if strings.Contains(modulePath, "@") {
					err1 = err
				}
				return fmt.Errorf("invalid module name %q: %v", modulePath, err1)
			}
			// Default major version to v0 if the modules experiment is enabled.
			if cueexperiment.Flags.Modules {
				modulePath += "@v0"
			}
		}
	}

	mod := filepath.Join(rootWorkingDir, "cue.mod")
	if info, err := os.Stat(mod); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("cue.mod files are no longer supported; use cue.mod/module.cue")
		}
		return fmt.Errorf("cue.mod directory already exists")
	}
	mf := &modfile.File{
		Module: modulePath,
	}
	if s := flagSource.String(cmd); s != "" {
		mf.Source = &modfile.Source{
			Kind: s,
		}
		if err := mf.Source.Validate(); err != nil {
			return err
		}
	}
	mf.Language = &modfile.Language{
		Version: cueversion.LanguageVersion(),
	}

	err = os.Mkdir(mod, 0755)
	if err != nil && !os.IsExist(err) {
		return err
	}

	data, err := mf.Format()
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(mod, "module.cue"), data, 0o666); err != nil {
		return err
	}

	if err = os.Mkdir(filepath.Join(mod, "usr"), 0755); err != nil {
		return err
	}
	if err = os.Mkdir(filepath.Join(mod, "pkg"), 0755); err != nil {
		return err
	}

	return err
}
