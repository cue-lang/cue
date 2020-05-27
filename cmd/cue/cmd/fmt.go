// Copyright 2018 The CUE Authors
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
	"github.com/spf13/cobra"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/internal/encoding"
	"cuelang.org/go/tools/fix"
)

func newFmtCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fmt [-s] [inputs]",
		Short: "formats CUE configuration files",
		Long: `Fmt formats the given files or the files for the given packages in place
`,
		RunE: mkRunE(c, func(cmd *Command, args []string) error {
			plan, err := parseArgs(cmd, args, &config{loadCfg: &load.Config{
				Tests: true,
				Tools: true,
			}})
			exitOnErr(cmd, err, true)

			opts := []format.Option{}
			if flagSimplify.Bool(cmd) {
				opts = append(opts, format.Simplify())
			}

			cfg := *plan.encConfig
			cfg.Format = opts
			cfg.Force = true

			for _, inst := range plan.insts {
				if inst.Err != nil {
					exitOnErr(cmd, inst.Err, false)
					continue
				}
				all := []*build.File{}
				all = append(all, inst.BuildFiles...)
				for _, name := range append(inst.ToolCUEFiles, inst.TestCUEFiles...) {
					all = append(all, &build.File{
						Filename: name,
						Encoding: build.CUE,
					})
				}
				for _, file := range all {
					files := []*ast.File{}
					d := encoding.NewDecoder(file, &cfg)
					defer d.Close()
					for ; !d.Done(); d.Next() {
						f := d.File()

						if file.Encoding == build.CUE {
							f = fix.File(f)
						}

						files = append(files, f)
					}

					e, err := encoding.NewEncoder(file, &cfg)
					exitOnErr(cmd, err, true)

					for _, f := range files {
						err := e.EncodeFile(f)
						exitOnErr(cmd, err, false)
					}
					e.Close()
				}
			}
			return nil
		}),
	}
	return cmd
}
