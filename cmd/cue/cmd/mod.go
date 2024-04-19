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
	gomodule "golang.org/x/mod/module"
	"golang.org/x/mod/semver"

	"cuelang.org/go/internal/cueexperiment"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/module"
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
			return ErrPrintedError
		}),
	}

	cmd.AddCommand(newModEditCmd(c))
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
	if info, err := os.Stat(mod); err == nil {
		if !info.IsDir() {
			return fmt.Errorf("cue.mod files are no longer supported; use cue.mod/module.cue")
		}
		return fmt.Errorf("cue.mod directory already exists")
	}
	mf := &modfile.File{
		Module: modulePath,
	}
	vers := versionForModFile()
	if vers == "" {
		// Shouldn't happen because we should use the
		// fallback version if we can't the version otherwise.
		return fmt.Errorf("cannot determine language version for module")
	}
	mf.Language = &modfile.Language{
		Version: vers,
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

func versionForModFile() string {
	version := cueVersion()
	earliestPossibleVersion := modfile.EarliestClosedSchemaVersion()
	if semver.Compare(version, earliestPossibleVersion) < 0 {
		// The reported version is earlier than it should be,
		// which can occur for some pseudo versions, or
		// potentially the cue command has been forked and
		// published under an independent version numbering.
		//
		// In this case, we use the fallback version as the best
		// guess as to a version that actually reflects the
		// capabilities of the module file.
		version = modfile.LatestKnownSchemaVersion()
	}
	if gomodule.IsPseudoVersion(version) {
		// If we have a version like v0.7.1-0.20240130142347-7855e15cb701
		// we want it to turn into the base version (v0.7.0 in that example).
		// Subject the resulting base version to the same sanity check
		// as above.
		pv, _ := gomodule.PseudoVersionBase(version)
		if pv != "" && semver.Compare(pv, earliestPossibleVersion) >= 0 {
			version = pv
		}
	}
	return version
}
