// Copyright 2024 The CUE Authors
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
	"slices"
	"strings"

	"cuelabs.dev/go/oci/ociregistry/ociref"
	"github.com/spf13/cobra"

	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/module"
)

func newModResolveCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resolve [<modulepath>[@<version>] ...]",
		Short: "Show how a module path resolves to a registry",
		Long: `This command prints information about how a given
module path will resolve to an actual registry in the
form of an OCI reference.

If the module version (which must be a canonical semver version)
is omitted, it omits the tag from the reference.

It only consults local information - it works lexically
with respect to the registry configuration (see "cue help registryconfig")
and does not make any network calls to check whether
the module exists.

If no arguments are provided, the current module path is used.
This is equivalent to specifying "." as an argument, which
also refers to the current module.

If the --all flag is provided, all dependencies from the current
module are resolved and displayed. The --all flag cannot be used
with module path arguments.

Note that this command is not yet stable and may be changed.
`,
		RunE: mkRunE(c, runModResolve),
	}
	cmd.Flags().BoolP("all", "a", false, "resolve all dependencies in the current module")
	return cmd
}

func runModResolve(cmd *Command, args []string) error {
	resolver, err := getRegistryResolver()
	if err != nil {
		return err
	}

	allFlag, err := cmd.Flags().GetBool("all")
	if err != nil {
		return err
	}

	// Validate that --all and args are mutually exclusive
	if allFlag && len(args) > 0 {
		return fmt.Errorf("cannot specify module arguments with --all flag")
	}

	// Handle --all flag: list all dependencies
	if allFlag {
		_, mf, _, err := readModuleFile()
		if err != nil {
			return err
		}

		if len(mf.Deps) == 0 {
			// No dependencies to resolve
			return nil
		}

		// Collect all module paths and sort them for consistent output
		var modulePaths []string
		for modPath := range mf.Deps {
			modulePaths = append(modulePaths, modPath)
		}
		slices.Sort(modulePaths)

		// Resolve and print each dependency
		for _, modPath := range modulePaths {
			dep := mf.Deps[modPath]
			mpath, _, ok := strings.Cut(modPath, "@")
			if !ok {
				mpath = modPath
			}

			// Always use the version from the dependency
			vers := dep.Version

			loc, ok := resolver.ResolveToLocation(mpath, vers)
			if !ok {
				return fmt.Errorf("no registry found for module %q", modPath)
			}

			ref := ociref.Reference{
				Host:       loc.Host,
				Repository: loc.Repository,
			}
			if vers != "" {
				ref.Tag = loc.Tag
			}

			// Print in format: module@version: registry-reference
			fmt.Printf("%s: %s\n", modPath, ref)
		}
		return nil
	}

	// Original behavior when --all is not specified
	var mf *modfile.File
	if len(args) == 0 {
		// Use the current module if no arguments are provided.
		args = []string{"."}
	}

	for _, arg := range args {
		if arg == "." {
			if mf == nil {
				var err error
				_, mf, _, err = readModuleFile()
				if err != nil {
					return err
				}
			}
			arg = mf.Module
		}

		mpath, vers, ok := strings.Cut(arg, "@")
		if ok {
			if _, err := module.ParseVersion(arg); err != nil {
				return fmt.Errorf("invalid module path: %v", err)
			}
		} else {
			mpath = arg
			if err := module.CheckPathWithoutVersion(arg); err != nil {
				return fmt.Errorf("invalid module path: %v", err)
			}
		}
		loc, ok := resolver.ResolveToLocation(mpath, vers)
		if !ok {
			// TODO should we print this and carry on anyway?
			// And perhaps return a silent error when we do that?
			return fmt.Errorf("no registry found for module %q", arg)
		}

		ref := ociref.Reference{
			Host:       loc.Host,
			Repository: loc.Repository,
		}
		// TODO when vers is empty, loc.Tag does actually contain the
		// tag prefix (if any) so we could potentially provide more info,
		// but it might be misleading so leave it out for now.
		// Also, there's no way in the standard OCI reference syntax to
		// indicate that a connection is insecure, so leave out that
		// info too. We could use our own syntax (+insecure) but
		// that arguably makes the output less useful as it won't be
		// acceptable to standard tooling.
		if vers != "" {
			ref.Tag = loc.Tag
		}
		fmt.Println(ref)
	}
	return nil
}
