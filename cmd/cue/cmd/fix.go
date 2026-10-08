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
	"io/fs"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/tools/fix"
	"github.com/spf13/cobra"
)

func newFixCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fix [flags] [packages]",
		Short: "rewrite packages to latest standards",
		Long: `Fix finds CUE programs that use old syntax and old APIs and rewrites them to use newer ones.
After you update to a new CUE release, fix helps make the necessary changes
to your program.

Without any packages, fix applies to all files within a module.


Experiments

CUE experiments are features that are not yet part of the stable language but
are being tested for future inclusion. Some of these may introduce backwards
incompatible changes for which there is a cue fix. The --exp flag is used to
change a file or package to use the new, experimental semantics. Experiments
are enabled on a per-file basis.

For example, to enable the "explicitopen" experiment for all files in a
package whose module is on a language version before v0.18.0, which is where
that experiment became stable, you would run:

	cue fix . --exp=explicitopen

For this to succeed, your current language version must support the experiment.
If an experiment has not yet been accepted for the current version, an
@experiment attribute is added in each affected file to mark the transition as
complete. An experiment which is already stable for that version needs no
fix, as files use it without an attribute, and asking for one is an error.

The special value --exp=all enables all experimental features that apply to the
current version.
`,
		RunE: mkRunE(c, runFixAll),
	}

	cmd.Flags().BoolP(string(flagForce), "f", false,
		"rewrite even when there are errors")

	cmd.Flags().StringSlice("exp", nil,
		"list of experiments to port")

	cmd.Flags().Bool("remove-list-commas", false,
		"remove commas from multiline list elements (v0.17.0+)")
	addCUEOutputFlags(cmd)

	return cmd
}

func runFixAll(cmd *Command, args []string) error {
	var opts []fix.Option
	if flagSimplify.Bool(cmd) {
		opts = append(opts, fix.Simplify())
	}

	if exps, err := cmd.Flags().GetStringSlice("exp"); err == nil && len(exps) > 0 {
		opts = append(opts, fix.Experiments(exps...))
	}

	if ok, err := cmd.Flags().GetBool("remove-list-commas"); err == nil && ok {
		opts = append(opts, fix.RemoveListCommas())
	}

	_, errs := fixInstances(cmd, args, flagForce.Bool(cmd), opts...)
	return errs
}

func fixInstances(cmd *Command, args []string, force bool, opts ...fix.Option) ([]*build.Instance, errors.Error) {
	if len(args) == 0 {
		args = []string{"./..."}

		dir, err := findModuleRoot()
		if err != nil {
			return nil, errors.Promote(err, "")
		}
		for _, sub := range []string{"gen", "pkg", "usr"} {
			args = appendDirs(args, filepath.Join(dir, "cue.mod", sub))
		}
	}

	instances := load.Instances(args, &load.Config{
		Tests:   true,
		Tools:   true,
		Package: "*",
	})

	errs := fix.Instances(instances, opts...)

	// An instance which fails to load contributes no files, so without this
	// the fixer would leave it alone and say nothing about it.
	for _, i := range instances {
		if i.Err != nil {
			errs = errors.Append(errs, errors.Promote(suggestModCommand(i.Err), ""))
		}
	}

	if errs != nil && !force {
		return nil, errs
	}

	done := map[*ast.File]bool{}

	for _, i := range instances {
		// Files is index-parallel with BuildFiles (see [build.Instance]),
		// so BuildFiles[fi].Source holds the raw bytes for Files[fi].
		for fi, f := range i.Files {
			if done[f] || (f.Filename != "-" && !strings.HasSuffix(f.Filename, ".cue")) {
				continue
			}
			done[f] = true

			b, err := format.Node(f)
			if err != nil {
				errs = errors.Append(errs, errors.Promote(err, "format"))
			}

			if f.Filename == "-" {
				if _, err := cmd.OutOrStdout().Write(b); err != nil {
					return nil, errors.Promote(err, "format")
				}
			} else {
				oldData, _ := i.BuildFiles[fi].Source.([]byte)
				if err := writeFileIfChanged(f.Filename, oldData, b, 0666); err != nil {
					errs = errors.Append(errs, errors.Promote(err, "write"))
				}
			}
		}
	}

	return instances, nil
}

// appendDirs appends the directories under base which hold any CUE files not
// ignored by the loader, such as "github.com/foo/bar" in a cue.mod tree.
// Intermediate directories such as "github.com/foo" are not packages, and
// loading them would fail. A directory directly under base is left out too,
// as its import path lacks a dot and names a standard library package.
// A directory which cannot be read is kept, so that the loader reports the
// error.
func appendDirs(a []string, base string) []string {
	seen := map[string]bool{}
	_ = filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
		dir := path
		if err == nil {
			if entry.IsDir() || !isCUEFile(entry.Name()) {
				return nil
			}
			dir = filepath.Dir(path)
		}
		if dir == base || seen[dir] {
			return nil
		}
		seen[dir] = true
		short := filepath.ToSlash(dir[len(base)+1:])
		if strings.Contains(short, "/") {
			a = append(a, short)
		}
		return nil
	})
	return a
}

// isCUEFile reports whether name is a CUE file which the loader does not
// ignore, such as those starting with an underscore.
func isCUEFile(name string) bool {
	return strings.HasSuffix(name, ".cue") &&
		!strings.HasPrefix(name, "_") && !strings.HasPrefix(name, ".")
}
