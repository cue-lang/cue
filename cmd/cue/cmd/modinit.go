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

	"github.com/spf13/cobra"

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

If the module name is not provided, a default module path (cue.example) will be
used.
`,
		RunE: mkRunE(c, runModInit),
	}

	cmd.Flags().BoolP(string(flagForce), "f", false, "force moving old-style cue.mod file")
	cmd.Flags().String(string(flagSource), "", "set the source field")
	cmd.Flags().String(string(flagLanguageVersion), "current", "set the language version ('current' means current language version)")
	return cmd
}

func runModInit(cmd *Command, args []string) (err error) {
	modulePath := "cue.example"
	if len(args) > 0 {
		if len(args) != 1 {
			return fmt.Errorf("too many arguments")
		}
		modulePath = args[0]
		if err := module.CheckPath(modulePath); err != nil {
			return err
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
	editFunc, err := addLanguageVersion(flagLanguageVersion.String(cmd))
	if err != nil {
		return err
	}
	if err := editFunc(mf); err != nil {
		return err
	}

	err = os.Mkdir(mod, 0777)
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

	if err = os.Mkdir(filepath.Join(mod, "usr"), 0777); err != nil {
		return err
	}
	if err = os.Mkdir(filepath.Join(mod, "pkg"), 0777); err != nil {
		return err
	}

	return err
}
