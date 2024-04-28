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

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/encoding"
	"cuelang.org/go/internal/filetypes"
	"cuelang.org/go/internal/source"
	"cuelang.org/go/tools/fix"

	"github.com/rogpeppe/go-internal/diff"
	"github.com/spf13/cobra"
)

func newFmtCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fmt [-s] [inputs]",
		Short: "formats CUE configuration files",
		Long: `Fmt formats the given files or the files for the given packages in place

Arguments are interpreted as import paths (see 'cue help inputs') unless --flags is set,
in which case the arguments are file paths to descend into and format all CUE files.
Directories named "cue.mod" and those beginning with "." and "_" are skipped unless
given as explicit arguments.
`,
		RunE: mkRunE(c, func(cmd *Command, args []string) error {
			cwd, _ := os.Getwd()
			check := flagCheck.Bool(cmd)

			f := formatter{
				ctx: cmd.ctx,
				cwd: cwd,
				onError: func(err error) {
					exitOnErr(cmd, err, false)
				},
				diff:   flagDiff.Bool(cmd),
				check:  check,
				stdout: cmd.OutOrStdout(),
			}

			if !flagFiles.Bool(cmd) { // format packages
				plan, err := newBuildPlan(cmd, &config{loadCfg: &load.Config{
					Tests:       true,
					Tools:       true,
					AllCUEFiles: true,
					Package:     "*",
				}})
				exitOnErr(cmd, err, true)
				builds := loadFromArgs(args, plan.cfg.loadCfg)
				if builds == nil {
					exitOnErr(cmd, errors.Newf(token.NoPos, "invalid args"), true)
				}

				cfg := plan.encConfig
				opts := []format.Option{}
				if flagSimplify.Bool(cmd) {
					opts = append(opts, format.Simplify())
				}
				cfg.Format = opts
				cfg.Force = true
				f.encConfig = cfg

				for _, inst := range builds {
					if inst.Err != nil {
						switch {
						case errors.As(inst.Err, new(*load.PackageError)):
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

						if err := f.format(file); err != nil {
							return err
						}
					}
				}
			} else { // format individual files
				i := slices.IndexFunc(args, func(arg string) bool {
					hasDots := slices.Contains(strings.Split(arg, string(filepath.Separator)), "...")
					return hasDots
				})
				if i != -1 {
					exitOnErr(cmd, errors.New(`cannot use "..." in --files mode`), true)
				}

				f.encConfig = encConfig(cmd)

				processFile := func(path string) error {
					file, err := filetypes.ParseFile(path, filetypes.Input)
					if err != nil {
						return err
					}

					if path == "-" {
						contents, err := io.ReadAll(cmd.InOrStdin())
						exitOnErr(cmd, err, false)
						file.Source = contents
					}

					return f.format(file)
				}
				for _, arg := range args {
					if arg == "-" {
						err := processFile(arg)
						exitOnErr(cmd, err, false)
						continue
					}

					switch info, err := os.Stat(arg); {
					case err != nil:
						exitOnErr(cmd, err, false)
					case !info.IsDir():
						err := processFile(arg)
						exitOnErr(cmd, err, false)
					default:
						err := filepath.WalkDir(arg, func(path string, d fs.DirEntry, err error) error {
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
							}

							if !strings.HasSuffix(path, ".cue") {
								return nil
							}

							return processFile(path)
						})
						exitOnErr(cmd, err, false)
					}
				}
			}

			if check && f.foundBadlyFormatted {
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

type formatter struct {
	encConfig *encoding.Config
	ctx       *cue.Context
	cwd       string
	onError   func(err error)
	diff      bool
	check     bool
	stdout    io.Writer

	foundBadlyFormatted bool
}

func (f *formatter) format(file *build.File) error {
	// We buffer the input and output bytes to compare them.
	// This allows us to determine whether a file is already
	// formatted, without modifying the file.
	var original []byte
	var formatted bytes.Buffer
	if bs, ok := file.Source.([]byte); ok {
		original = bs
	} else {
		var err error
		original, err = source.ReadAll(file.Filename, file.Source)
		if err != nil {
			return err
		}
		file.Source = original
	}
	f.encConfig.Out = &formatted
	if file.Filename == "-" && !f.diff && !f.check {
		// Always write to stdout if the file is read from stdin.
		f.encConfig.Out = io.MultiWriter(f.encConfig.Out, f.stdout)
	}

	var files []*ast.File
	d := encoding.NewDecoder(f.ctx, file, f.encConfig)
	defer d.Close()
	for ; !d.Done(); d.Next() {
		f := d.File()

		if file.Encoding == build.CUE {
			f = fix.File(f)
		}

		files = append(files, f)
	}
	if err := d.Err(); err != nil {
		return err
	}

	e, err := encoding.NewEncoder(f.ctx, file, f.encConfig)
	if err != nil {
		return err
	}

	for _, s := range files {
		err := e.EncodeFile(s)
		f.onError(err)
	}

	if err := e.Close(); err != nil {
		return err
	}

	// File is already well formatted; we can stop here.
	if bytes.Equal(formatted.Bytes(), original) {
		return nil
	}

	f.foundBadlyFormatted = true
	name := file.Filename
	path, err := filepath.Rel(f.cwd, name)
	if err != nil {
		path = name
	}

	switch {
	case f.diff:
		d := diff.Diff(path+".orig", original, path, formatted.Bytes())
		fmt.Fprintln(f.stdout, string(d))
	case f.check:
		fmt.Fprintln(f.stdout, path)
	case file.Filename == "-":
		// already written to stdout during encoding
	default:
		err := os.WriteFile(file.Filename, formatted.Bytes(), 0644)
		f.onError(err)
	}

	return nil
}
