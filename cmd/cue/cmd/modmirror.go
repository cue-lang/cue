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
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"cuelang.org/go/internal/mod/modload"
	"cuelang.org/go/internal/mod/semver"
	"cuelang.org/go/mod/modconfig"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/modregistry"
	"cuelang.org/go/mod/module"
)

func newModMirrorCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mirror [module...]",
		Short: "mirror module content between registries",
		Long: `WARNING: THIS COMMAND IS EXPERIMENTAL.

This commmand ensures that a set of modules and their dependencies
are available ("mirrored") in a registry.

For each module specified on the command line, it ensures that the
module and all the modules in depends on
are present in both the "from" registry and the "to" registry and that the
contents are the same in each. If the --no-deps is specified, only
the module itself but not its dependencies will be mirrored.

A module may be specified as <module>@<version>, in which case the
specified version will be mirrored. If the version is canonical (for example v1.2.3), then
exactly that version will be mirrored, otherwise the latest corresponding
version will be mirrored (or all corresponding versions if --all-versions
is specified).

For example:

	# Copy from $CUE_REGISTRY (usually the Central Registry) to my.registry.example
	cue mod mirror --to my.registry.example foo.com/m1@v1.2.3 bar.org@v2

will copy the exact module foo.com/m1@v1.2.3 but the latest version
of bar.org@2, or all v2.x.y versions if --all-versions is given.
If no major version is specified, the latest major version will be chosen.

By default the latest version is chosen by consulting the source registry,
unless the --mod flag is specified, in which case the current module's
dependencies will be used. When --mod is given and no modules
are specified on the command line, all the current module's dependencies will
be mirrored.

Note that this command is not yet stable and may be changed.
`,
		RunE: mkRunE(c, runModMirror),
	}
	cmd.Flags().BoolP(string(flagDryRun), "n", false, "only run simulation")
	cmd.Flags().Bool(string(flagNoDeps), false, "do not copy module dependencies")
	cmd.Flags().String(string(flagFrom), "", "source registry (defaults to $CUE_REGISTRY)")
	cmd.Flags().String(string(flagTo), "", "destination registry (defaults to $CUE_REGISTRY)")
	cmd.Flags().BoolP(string(flagAllVersions), "a", false, "copy all available versions of the specified modules")
	cmd.Flags().BoolP(string(flagMod), "m", false, "mirror the current main module's dependency modules by default")

	return cmd
}

func runModMirror(cmd *Command, args []string) error {
	ctx := cmd.Context()
	//dryRun := flagDryRun.Bool(cmd)	// TODO
	noDeps := flagNoDeps.Bool(cmd)
	srcRegStr := flagFrom.String(cmd)
	dstRegStr := flagTo.String(cmd)
	allVersions := flagAllVersions.Bool(cmd)
	useMod := flagMod.Bool(cmd)

	// TODO configure concurrency limit?

	srcResolver, err := modconfig.NewResolver(newModConfig(srcRegStr))
	if err != nil {
		return err
	}
	srcReg := modregistry.NewClientWithResolver(srcResolver)

	dstResolver, err := modconfig.NewResolver(newModConfig(dstRegStr))
	if err != nil {
		return err
	}
	dstReg := modregistry.NewClientWithResolver(dstResolver)

	var mf *modfile.File
	if useMod {
		// Read current module to get dependencies.
		var err error
		_, mf, _, err = readModuleFile()
		if err != nil {
			return err
		}
	}
	// If no modules specified, possibly mirror from the current module if --mod is set.
	modules := args
	if len(modules) == 0 && useMod {
		deps := mf.DepVersions()
		modules = make([]string, 0, len(deps))
		for _, dv := range deps {
			if allVersions {
				// Note: Path includes the major version, but it seems reasonable
				// to restrict module dependencies to the same major version.
				modules = append(modules, dv.Path())
			} else {
				modules = append(modules, dv.String())
			}
		}
	}
	if len(modules) == 0 {
		// Nothing to do.
		return nil
	}

	// First expand the module list to list specific versions of all the
	// initial modules to copy.
	// TODO concurrency
	var expanded []module.Version
	for _, m := range modules {
		mpath, mvers, ok := module.SplitPathVersion(m)
		if !ok || semver.Canonical(mvers) != mvers {
			if useMod {
				// Resolve the version from the module file.
				mv, ok := mf.ModuleForImportPath(mpath)
				if !ok {
					return fmt.Errorf("no version for %q found in module file", mpath)
				}
				expanded = append(expanded, mv)
				continue
			}
			versions, err := srcReg.ModuleVersions(ctx, m)
			if err != nil {
				return err
			}
			if len(versions) == 0 {
				return fmt.Errorf("no versions found for module %v", m)
			}
			if allVersions {
				for _, v := range versions {
					mv, err := module.NewVersion(mpath, v)
					if err != nil {
						return err
					}
					expanded = append(expanded, mv)
				}
			} else {
				mv, err := module.NewVersion(mpath, modload.LatestVersion(versions))
				if err != nil {
					return err
				}
				expanded = append(expanded, mv)
			}
		} else {
			mv, err := module.ParseVersion(m)
			if err != nil {
				return err
			}
			expanded = append(expanded, mv)
		}
	}

	// Now copy the modules and their dependencies recursively, depth-first.
	mm := &modMirror{
		allVersions: allVersions,
		srcReg:      srcReg,
		dstReg:      dstReg,
		noDeps:      noDeps,
		done:        make(map[module.Version]bool),
	}
	// TODO concurrency
	for _, m := range expanded {
		if err := mm.mirrorWithDeps(ctx, m); err != nil {
			return err
		}
	}
	return nil
}

type modMirror struct {
	allVersions bool
	srcReg      *modregistry.Client
	dstReg      *modregistry.Client
	done        map[module.Version]bool
	noDeps      bool
}

func (mm *modMirror) mirrorWithDeps(ctx context.Context, mv module.Version) error {
	mm.done[mv] = true
	m, err := mm.srcReg.GetModule(ctx, mv)
	if err != nil {
		return err
	}
	modFileData, err := m.ModuleFile(ctx)
	if err != nil {
		return err
	}
	mf, err := modfile.Parse(modFileData, mv.String())
	if err != nil {
		return err
	}
	if !mm.noDeps {
		// TODO technically this can copy more than is strictly necessary
		// when we're operating in module mode, because the main
		// module will only require the latest version of any of its dependencies,
		// but those individual dependencies may themselves require
		// earlier versions of those modules.
		// It's safer to do things this way as it means that we're guaranteed
		// that every individual module in the target registry has all its
		// dependencies present, but there could be room for a mode that
		// does a more parsimonious copy.
		for _, dep := range mf.DepVersions() {
			if mm.done[dep] {
				continue
			}
			if err := mm.mirrorWithDeps(ctx, dep); err != nil {
				return err
			}
		}
	}
	fmt.Printf("mirroring %v\n", mv)
	if err := mm.srcReg.Mirror(ctx, mm.dstReg, mv); err != nil {
		return err
	}
	return nil
}
