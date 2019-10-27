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
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

func newModCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mod <cmd> [arguments]",
		Short: "module maintenace",
		Long: `
`,
		RunE: mkRunE(c, func(cmd *Command, args []string) error {
			stderr := cmd.Stderr()
			if len(args) == 0 {
				fmt.Fprintln(stderr, "mod must be run as one of its subcommands")
			} else {
				fmt.Fprintf(stderr, "get must be run as one of its subcommands: unknown subcommand %q\n", args[0])
			}
			fmt.Fprintln(stderr, "Run 'cue help mod' for known subcommands.")
			os.Exit(1) // TODO: get rid of this
			return nil
		}),
	}

	cmd.AddCommand(newModInitCmd(c))
	return cmd
}

func newModInitCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init [module]",
		Short: "initialize new module in current director",
		Long: `Init initializes a cue.mod directory in the current directory,
in effect creating a new module rooted at the current directory.
The cue.mod directory must not already exist.
A legacy cue.mod file in the current directory is moved
to the new subdirectory.

A module name is optional, but if it is not given a packages
within the module cannot imported another package defined
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

	module := ""
	if len(args) > 0 {
		if len(args) != 1 {
			return fmt.Errorf("too many arguments")
		}
		module = args[0]
		u, err := url.Parse("https://" + module)
		if err != nil {
			return fmt.Errorf("invalid module name: %v", module)
		}
		if h := u.Hostname(); !strings.Contains(h, ".") {
			return fmt.Errorf("invalid host name %q", h)
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

	err = os.Mkdir(mod, 0755)
	if err != nil && !os.IsExist(err) {
		return err
	}

	f, err := os.Create(filepath.Join(mod, "module.cue"))
	if err != nil {
		return err
	}
	defer f.Close()

	// Set module even if it is empty, making it easier for users to fill it in.
	_, err = fmt.Fprintf(f, "module: %q\n", module)

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
