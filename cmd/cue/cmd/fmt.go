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
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rogpeppe/go-internal/diff"
	"github.com/spf13/cobra"

	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/filetypes"
	"cuelang.org/go/internal/source"
)

func newFmtCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fmt [-s] [inputs]",
		Short: "formats CUE configuration files",
		Long: `Fmt formats the given files or the files for the given packages in place

Arguments are interpreted as import paths (see 'cue help inputs') unless --files is set,
in which case the arguments are file paths to descend into and format all CUE files.
Directories named "cue.mod" and those beginning with "." and "_" are skipped unless
given as explicit arguments.
`,
		RunE: mkRunE(c, func(cmd *Command, args []string) error {
			check := flagCheck.Bool(cmd)
			doDiff := flagDiff.Bool(cmd)

			formatOpts := []format.Option{}
			if flagSimplify.Bool(cmd) {
				formatOpts = append(formatOpts, format.Simplify())
			}

			var foundBadlyFormatted bool
			if !flagFiles.Bool(cmd) { // format packages
				builds := loadFromArgs(args, &load.Config{
					Tests:       true,
					Tools:       true,
					AllCUEFiles: true,
					Package:     "*",
					SkipImports: true,
				})
				if len(builds) == 0 {
					return errors.Newf(token.NoPos, "invalid args")
				}
				for _, inst := range builds {
					if err := inst.Err; err != nil {
						return err
					}
					for _, file := range inst.BuildFiles {
						shouldFormat := inst.User || file.Filename == "-" || filepath.Dir(file.Filename) == inst.Dir
						if !shouldFormat {
							continue
						}

						wasModified, err := formatFile(file, formatOpts, doDiff, check, cmd)
						if err != nil {
							return err
						}
						if wasModified {
							foundBadlyFormatted = true
						}
					}
				}
			} else { // format individual files
				hasDots := slices.ContainsFunc(args, func(arg string) bool {
					return strings.Contains(arg, "...")
				})
				if hasDots {
					return errors.New(`cannot use "..." in --files mode`)
				}

				if len(args) == 0 {
					args = []string{"."}
				}

				processFile := func(path string) error {
					file, err := filetypes.ParseFile(path, filetypes.Input)
					if err != nil {
						return err
					}

					if path == "-" {
						contents, err := io.ReadAll(cmd.InOrStdin())
						if err != nil {
							return err
						}
						file.Source = contents
					}

					wasModified, err := formatFile(file, formatOpts, doDiff, check, cmd)
					if err != nil {
						return err
					}
					if wasModified {
						foundBadlyFormatted = true
					}
					return nil
				}
				for _, arg := range args {
					if arg == "-" {
						if err := processFile(arg); err != nil {
							return err
						}
						continue
					}

					arg = filepath.Clean(arg)
					if err := filepath.WalkDir(arg, func(path string, d fs.DirEntry, err error) error {
						if err != nil {
							return err
						}

						if d.IsDir() {
							name := d.Name()
							isMod := name == "cue.mod"
							isDot := strings.HasPrefix(name, ".") && name != "." && name != ".."
							if path != arg && (isMod || isDot || strings.HasPrefix(name, "_")) {
								return filepath.SkipDir
							}
							return nil
						}

						if !strings.HasSuffix(path, ".cue") {
							return nil
						}

						return processFile(path)
					}); err != nil {
						return err
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
	cmd.Flags().Bool(string(flagFiles), false, "treat arguments as file paths to descend into rather than import paths")

	return cmd
}

// formatFile formats a single file.
// It returns true if the file was not well formatted.
func formatFile(file *build.File, opts []format.Option, doDiff, check bool, cmd *Command) (bool, error) {
	// We buffer the input and output bytes to compare them.
	// This allows us to determine whether a file is already
	// formatted, without modifying the file.
	src, ok := file.Source.([]byte)
	if !ok {
		var err error
		src, err = source.ReadAll(file.Filename, file.Source)
		if err != nil {
			return false, err
		}
	}

	syntax, err := parser.ParseFile(file.Filename, src, parser.ParseComments)
	if err != nil {
		return false, err
	}

	formatted, err := format.Node(syntax, opts...)
	if err != nil {
		return false, err
	}

	stdout := cmd.OutOrStdout()
	// Always write to stdout if the file is read from stdin.
	if file.Filename == "-" && !doDiff && !check {
		stdout.Write(formatted)
	}

	// File is already well formatted; we can stop here.
	if bytes.Equal(formatted, src) {
		return false, nil
	}

	path, err := filepath.Rel(rootWorkingDir, file.Filename)
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
		if err := os.WriteFile(file.Filename, formatted, 0666); err != nil {
			return false, err
		}
	}
	return true, nil
}
