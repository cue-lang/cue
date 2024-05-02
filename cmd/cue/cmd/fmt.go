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
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/source"

	"github.com/rogpeppe/go-internal/diff"
	"github.com/spf13/cobra"
)

func newFmtCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fmt [-s] [inputs]",
		Short: "formats CUE configuration files",
		Long: `Fmt formats the given files or the files for the given packages in place
`,
		RunE: mkRunE(c, func(cmd *Command, args []string) error {
			builds := loadFromArgs(args, &load.Config{
				Tests:       true,
				Tools:       true,
				AllCUEFiles: true,
				Package:     "*",
			})
			if builds == nil {
				exitOnErr(cmd, errors.Newf(token.NoPos, "invalid args"), true)
			}

			opts := []format.Option{}
			if flagSimplify.Bool(cmd) {
				opts = append(opts, format.Simplify())
			}

			var foundBadlyFormatted bool
			check := flagCheck.Bool(cmd)
			doDiff := flagDiff.Bool(cmd)
			cwd, _ := os.Getwd()
			stdout := cmd.OutOrStdout()

			for _, inst := range builds {
				if inst.Err != nil {
					switch {
					case errors.As(inst.Err, new(*load.PackageError)) && len(inst.BuildFiles) != 0:
						// Ignore package errors if there are files to format.
					case errors.As(inst.Err, new(*load.NoFilesError)):
					default:
						exitOnErr(cmd, inst.Err, false)
						continue
					}
				}
				for _, file := range inst.BuildFiles {
					shouldFormat := inst.User || file.Filename == "-" || filepath.Dir(file.Filename) == inst.Dir
					if !shouldFormat {
						continue
					}

					// We buffer the input and output bytes to compare them.
					// This allows us to determine whether a file is already
					// formatted, without modifying the file.
					src, ok := file.Source.([]byte)
					if !ok {
						var err error
						src, err = source.ReadAll(file.Filename, file.Source)
						exitOnErr(cmd, err, true)
					}

					file, err := parser.ParseFile(file.Filename, src, parser.ParseComments)
					exitOnErr(cmd, err, true)

					formatted, err := format.Node(file, opts...)
					exitOnErr(cmd, err, true)

					// Always write to stdout if the file is read from stdin.
					if file.Filename == "-" && !doDiff && !check {
						stdout.Write(formatted)
					}

					// File is already well formatted; we can stop here.
					if bytes.Equal(formatted, src) {
						continue
					}

					foundBadlyFormatted = true
					path, err := filepath.Rel(cwd, file.Filename)
					if err != nil {
						path = file.Filename
					}

					switch {
					case doDiff:
						d := diff.Diff(path+".orig", src, path, formatted)
						fmt.Fprintln(stdout, string(d))
					case check:
						fmt.Fprintln(stdout, path)
					case file.Filename == "-":
						// already wrote the formatted source to stdout above
					default:
						if err := os.WriteFile(file.Filename, formatted, 0644); err != nil {
							exitOnErr(cmd, err, false)
						}
					}
				}
			}

			if check && foundBadlyFormatted {
				return ErrPrintedError
			}

			return nil
		}),
	}

	cmd.Flags().Bool(string(flagCheck), false, "exits with non-zero status if any files are not formatted")
	cmd.Flags().BoolP(string(flagDiff), "d", false, "display diffs instead of rewriting files")

	return cmd
}
