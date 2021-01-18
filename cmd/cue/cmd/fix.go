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
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue/ast"
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
`,
		RunE: mkRunE(c, runFixAll),
	}

	cmd.Flags().BoolP(string(flagForce), "f", false,
		"rewrite even when there are errors")

	return cmd
}

func runFixAll(cmd *Command, args []string) error {
	dir, err := os.Getwd()
	if err != nil {
		return err
	}

	var opts []fix.Option
	if flagSimplify.Bool(cmd) {
		opts = append(opts, fix.Simplify())
	}

	if len(args) == 0 {
		args = []string{"./..."}

		for {
			if fi, err := os.Stat(filepath.Join(dir, "cue.mod")); err == nil {
				if fi.IsDir() {
					args = appendDirs(args, filepath.Join(dir, "cue.mod", "gen"))
					args = appendDirs(args, filepath.Join(dir, "cue.mod", "pkg"))
					args = appendDirs(args, filepath.Join(dir, "cue.mod", "usr"))
				} else {
					args = appendDirs(args, filepath.Join(dir, "pkg"))
				}
				break
			}

			dir = filepath.Dir(dir)
			if info, _ := os.Stat(dir); !info.IsDir() {
				return errors.Newf(token.NoPos, "no module root found")
			}
		}
	}

	instances := load.Instances(args, &load.Config{
		Tests: true,
		Tools: true,
	})

	errs := fix.Instances(instances, opts...)

	if errs != nil && flagForce.Bool(cmd) {
		return errs
	}

	done := map[*ast.File]bool{}

	for _, i := range instances {
		for _, f := range i.Files {
			if done[f] || !strings.HasSuffix(f.Filename, ".cue") {
				continue
			}
			done[f] = true

			b, err := format.Node(f)
			if err != nil {
				errs = errors.Append(errs, errors.Promote(err, "format"))
			}

			err = ioutil.WriteFile(f.Filename, b, 0644)
			if err != nil {
				errs = errors.Append(errs, errors.Promote(err, "write"))
			}
		}
	}

	return errs
}

func appendDirs(a []string, base string) []string {
	_ = filepath.Walk(base, func(path string, fi os.FileInfo, err error) error {
		if err == nil && fi.IsDir() && path != base {
			short := filepath.ToSlash(path[len(base)+1:])
			if strings.ContainsAny(short, "/") {
				a = append(a, short)
			}
		}
		return nil
	})
	return a
}
