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
	"os"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/tools/fix"
	"github.com/spf13/cobra"
)

func newFixCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fix [packages]",
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

For example, to enable the "explicitopen" experiment for all files in a package,
you would run:

	cue fix . --exp=explicitopen

For this to succeed, your current language version must support the experiment.
If an experiment has not yet been accepted for the current version, an
@experiment attribute is added in each affected file to mark the transition as
complete.

The special value --exp=all enables all experimental features that apply to the
current version.
`,
		RunE: mkRunE(c, runFixAll),
	}

	cmd.Flags().BoolP(string(flagForce), "f", false,
		"rewrite even when there are errors")

	cmd.Flags().StringSlice("exp", nil,
		"list of experiments to port")

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

	_, errs := fixInstances(cmd, args, flagForce.Bool(cmd), opts...)
	return errs
}

func fixInstances(cmd *Command, args []string, force bool, opts ...fix.Option) ([]*build.Instance, errors.Error) {
	if len(args) == 0 {
		args = []string{"./..."}

		dir := rootWorkingDir()
		for {
			if _, err := os.Stat(filepath.Join(dir, "cue.mod")); err == nil {
				args = appendDirs(args, filepath.Join(dir, "cue.mod", "gen"))
				args = appendDirs(args, filepath.Join(dir, "cue.mod", "pkg"))
				args = appendDirs(args, filepath.Join(dir, "cue.mod", "usr"))
				break
			}

			dir = filepath.Dir(dir)
			if info, _ := os.Stat(dir); !info.IsDir() {
				return nil, errors.Newf(token.NoPos, "no module root found")
			}
		}
	}

	instances := load.Instances(args, &load.Config{
		Tests:   true,
		Tools:   true,
		Package: "*",
	})

	errs := fix.Instances(instances, opts...)

	if errs != nil && !force {
		return nil, errs
	}

	done := map[*ast.File]bool{}

	for _, i := range instances {
		for _, f := range i.Files {
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
				if err := os.WriteFile(f.Filename, b, 0666); err != nil {
					errs = errors.Append(errs, errors.Promote(err, "write"))
				}
			}
		}
	}

	return instances, nil
}

func appendDirs(a []string, base string) []string {
	_ = filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry.IsDir() && path != base {
			short := filepath.ToSlash(path[len(base)+1:])
			if strings.ContainsAny(short, "/") {
				a = append(a, short)
			}
		}
		return nil
	})
	return a
}
