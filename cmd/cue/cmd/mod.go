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
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"cuelang.org/go/internal/cueexperiment"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/module"
	gomodule "golang.org/x/mod/module"
)

func newModCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mod <cmd> [arguments]",
		Short: "module maintenance",
		Long: `Mod groups commands which operate on CUE modules.

Note that support for modules is built into all the cue commands, not
just 'cue mod'.

See also:
	cue help modules
`,
		RunE: mkRunE(c, func(cmd *Command, args []string) error {
			stderr := cmd.Stderr()
			if len(args) == 0 {
				fmt.Fprintln(stderr, "mod must be run as one of its subcommands")
			} else {
				fmt.Fprintf(stderr, "mod must be run as one of its subcommands: unknown subcommand %q\n", args[0])
			}
			fmt.Fprintln(stderr, "Run 'cue help mod' for known subcommands.")
			os.Exit(1) // TODO: get rid of this
			return nil
		}),
	}

	cmd.AddCommand(newModGetCmd(c))
	cmd.AddCommand(newModInitCmd(c))
	cmd.AddCommand(newModRegistryCmd(c))
	cmd.AddCommand(newModTidyCmd(c))
	cmd.AddCommand(newModUploadCmd(c))
	return cmd
}

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

	return cmd
}

func runModInit(cmd *Command, args []string) (err error) {
	defer func() {
		if err != nil {
			// TODO: Refactor Cobra usage to do something more principled
			fmt.Fprintln(cmd.OutOrStderr(), err)
			os.Exit(1)
		}
	}()

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

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	mod := filepath.Join(cwd, "cue.mod")

	info, err := os.Stat(mod)

	// Detect old setups and backport it if requested.
	if err == nil && !info.IsDir() {
		// This path backports
		if !flagForce.Bool(cmd) {
			return fmt.Errorf("detected old-style config file; use --force to upgrade")
		}
		return backport(mod, cwd)
	}

	if err == nil {
		return fmt.Errorf("cue.mod directory already exists")
	}
	mf := &modfile.File{
		Module: modulePath,
	}
	if vers := versionForModFile(); vers != "" {
		mf.Language = &modfile.Language{
			Version: vers,
		}
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

// backport backports an old cue.mod setup to a new one.
func backport(mod, cwd string) error {
	tmp := filepath.Join(cwd, fmt.Sprintf("_%x_cue.mod", rand.Int()))
	err := os.Rename(mod, tmp)
	if err != nil {
		return err
	}

	err = os.Mkdir(filepath.Join(cwd, "cue.mod"), 0755)
	if err != nil {
		os.Rename(tmp, mod)
		return err
	}

	err = os.Rename(tmp, filepath.Join(cwd, "cue.mod", "module.cue"))
	if err != nil {
		return err
	}

	err = os.Rename(filepath.Join(cwd, "pkg"), filepath.Join(cwd, "cue.mod", "gen"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	if err = os.Mkdir(filepath.Join(mod, "usr"), 0755); err != nil {
		return err
	}
	if err = os.Mkdir(filepath.Join(mod, "pkg"), 0755); err != nil {
		return err
	}

	return nil
}

func versionForModFile() string {
	version := cueVersion()
	if gomodule.IsPseudoVersion(version) {
		// If we have a version like v0.7.1-0.20240130142347-7855e15cb701
		// we want it to turn into the base version (v0.7.0 in that example).
		// If there's no base version (e.g. v0.0.0-...) then PseudoVersionBase
		// will return the empty string, which is exactly what we want
		// because we don't want to put v0.0.0 in a module.cue file.
		version, _ = gomodule.PseudoVersionBase(version)
	}
	return version
}
