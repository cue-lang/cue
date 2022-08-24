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
	"golang.org/x/text/message"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/encoding"
	"cuelang.org/go/tools/fix"
)

const (
	flagFmtList flagName = "list"
	flagFmtDiff flagName = "diff"
)

func newFmtCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fmt [-s] [-l] [-d] [inputs]",
		Short: "formats CUE configuration files",
		Long: `Fmt formats the given files or the files for the given packages in place
`,
		RunE: mkRunE(c, func(cmd *Command, args []string) error {
			plan, err := newBuildPlan(cmd, args, &config{loadCfg: &load.Config{
				Tests:       true,
				Tools:       true,
				AllCUEFiles: true,
				Package:     "*",
			}})
			exitOnErr(cmd, err, true)

			builds := loadFromArgs(cmd, args, plan.cfg.loadCfg)
			if builds == nil {
				exitOnErr(cmd, errors.Newf(token.NoPos, "invalid args"), true)
			}

			opts := []format.Option{}
			if flagSimplify.Bool(cmd) {
				opts = append(opts, format.Simplify())
			}

			listFileNamesEnabled := flagFmtList.Bool(cmd)

			cfg := *plan.encConfig
			cfg.Format = opts
			cfg.Force = true
			cfg.StaleIfNotModified = true
			cfg.DiffViewEnabled = flagFmtDiff.Bool(cmd)

			p := message.NewPrinter(getLang())

			for _, inst := range builds {
				if inst.Err != nil {
					var p *load.PackageError
					switch {
					case errors.As(inst.Err, &p):
					default:
						exitOnErr(cmd, inst.Err, false)
						continue
					}
				}
				for _, file := range inst.BuildFiles {
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

					if file.Modified && listFileNamesEnabled {
						_, _ = p.Println(inst.RelPath(file))
					}
					if file.Modified && cfg.DiffViewEnabled && !listFileNamesEnabled {
						_, _ = p.Printf("> %s\n", inst.RelPath(file))
						_, _ = p.Print(file.Diff)
					}
				}
			}
			return nil
		}),
	}

	cmd.Flags().BoolP(string(flagFmtList), "l", false, "list file names which would be/were formatted")
	cmd.Flags().BoolP(string(flagFmtDiff), "d", false, "show diffs which `fmt` would make without writing files")
	return cmd
}
