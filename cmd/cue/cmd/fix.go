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
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/mod/modfile"
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
`,
		RunE: mkRunE(c, runFixAll),
	}

	cmd.Flags().BoolP(string(flagForce), "f", false,
		"rewrite even when there are errors")

	cmd.Flags().StringSlice("exp", nil,
		"list of experiments to port")

	cmd.Flags().String("upgrade", "",
		"upgrade language version and apply accepted experiments (e.g., --upgrade=v0.16.0)")

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

	if upgradeVersion, err := cmd.Flags().GetString("upgrade"); err == nil && upgradeVersion != "" {
		opts = append(opts, fix.UpgradeVersion(upgradeVersion))
	}

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
				return errors.Newf(token.NoPos, "no module root found")
			}
		}
	}

	instances := load.Instances(args, &load.Config{
		Tests:   true,
		Tools:   true,
		Package: "*",
	})

	errs := fix.Instances(instances, opts...)

	if errs != nil && flagForce.Bool(cmd) {
		return errs
	}

	// Write updated module files to disk if upgrade was requested
	if upgradeVersion, _ := cmd.Flags().GetString("upgrade"); upgradeVersion != "" {
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
					return err
				}
			} else {
				if err := os.WriteFile(f.Filename, b, 0666); err != nil {
					errs = errors.Append(errs, errors.Promote(err, "write"))
				}
			}
		}
	}

	return errs
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
