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
	"cmp"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/rogpeppe/go-internal/diff"
	"github.com/spf13/cobra"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/filetypes"
	"cuelang.org/go/internal/mod/langversion"
	"cuelang.org/go/internal/source"
	"cuelang.org/go/mod/modfile"
)

func newFmtCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fmt [flags] [inputs]",
		Short: "formats CUE configuration files",
		Long: `
fmt rewrites CUE files in the standard format.

Arguments are interpreted as import paths (see 'cue help inputs') and all CUE files
in them are formatted, including those behind build attributes or without package names.

Use --files to treat arguments as paths to files or directories to recursively format.
--files cannot be combined with --language-version.
Directories named "cue.mod" and those beginning with "." and "_" are skipped unless
given as explicit arguments.

Examples:

  # Format stdin
  cue fmt -

  # Fail with a diff if a subset of packages needs formatting
  cue fmt --diff ./foo/...

  # Fail with a list of files if any CUE file in a whole repository needs formatting
  cue fmt --check --files .
`[1:],
		RunE: mkRunE(c, func(cmd *Command, args []string) error {
			check := flagCheck.Bool(cmd)
			doDiff := flagDiff.Bool(cmd)

			formatOpts := []format.Option{}
			if flagSimplify.Bool(cmd) {
				formatOpts = append(formatOpts, format.Simplify())
			}

			langVersion, err := languageVersionFlag(cmd.Flags())
			if err != nil {
				return err
			}

			var foundBadlyFormatted bool
			if !flagFiles.Bool(cmd) { // format packages
				builds := loadFromArgs(args, &load.Config{
					Tests:           true,
					Tools:           true,
					AllCUEFiles:     true,
					Package:         "*",
					SkipImports:     true,
					LanguageVersion: langVersion,
				})
				if len(builds) == 0 {
					return errors.Newf(token.NoPos, "invalid args")
				}
				for _, inst := range builds {
					if err := inst.Err; err != nil {
						return err
					}
					version := cmp.Or(inst.LanguageVersion(), langVersion)
					for _, file := range inst.BuildFiles {
						shouldFormat := inst.User || file.Filename == "-" || filepath.Dir(file.Filename) == inst.Dir
						if !shouldFormat {
							continue
						}

						wasModified, err := formatFile(file, formatOpts, version, doDiff, check, cmd)
						if err != nil {
							return err
						}
						if wasModified {
							foundBadlyFormatted = true
						}
					}
				}
			} else { // format individual files
				if langVersion != "" {
					return errors.New("cannot combine --files with --language-version")
				}
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

					wasModified, err := formatFile(file, formatOpts, langVersion, doDiff, check, cmd)
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
	cmd.Flags().Bool(string(flagFiles), false, "treat arguments as paths to files or directories to recursively format")
	addCUEOutputFlags(cmd)
	addLanguageVersionFlag(cmd)

	return cmd
}

// owningModuleFile returns the module file of the module that the file at
// path belongs to, by the rule of cue/load, shown relative to the working
// directory when it lies below it, and the language version it declares. It
// returns an empty module file if the file belongs to no module.
func owningModuleFile(path string) (modFile, declared string, _ error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", "", err
	}
	root := loaderModuleRoot(filepath.Dir(abs))
	if root == "" {
		return "", "", nil
	}
	modFile = filepath.Join(root, "cue.mod", "module.cue")
	data, err := os.ReadFile(modFile)
	if err != nil {
		return "", "", nil // a cue.mod without a module file does not make a module
	}
	if mf, err := modfile.ParseNonStrict(data, modFile); err == nil && mf.Language != nil {
		declared = mf.Language.Version
	}
	if rel, err := filepath.Rel(rootWorkingDir(), modFile); err == nil && !strings.HasPrefix(rel, "..") {
		modFile = filepath.ToSlash(rel)
	}
	return modFile, declared, nil
}

// checkVersionAttr reports an error if syntax, the file at path, carries a
// @version attribute while it belongs to a module that declares a language
// version, as cue/load does for the files it loads.
func checkVersionAttr(path string, syntax *ast.File) error {
	a := internal.VersionAttr(syntax)
	if a == nil || path == "-" {
		return nil
	}
	modFile, declared, err := owningModuleFile(path)
	if err != nil || modFile == "" || declared == "" {
		return err
	}
	return langversion.AttrInModuleError(a.Pos(), modFile, declared)
}

// loaderModuleRoot returns the module root that cue/load finds for files in
// dir: the nearest ancestor of dir, or dir itself, holding a cue.mod entry.
// It returns "" if there is none, or if the walk leaves a cue.mod directory,
// as files within cue.mod belong to no module.
func loaderModuleRoot(dir string) string {
	for {
		if _, err := os.Stat(filepath.Join(dir, "cue.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if filepath.Base(parent) == "cue.mod" || parent == dir {
			return ""
		}
		dir = parent
	}
}

// formatFile formats a single file.
// It returns true if the file was not well formatted.
func formatFile(file *build.File, opts []format.Option, version string, doDiff, check bool, cmd *Command) (bool, error) {
	// We buffer the input and output bytes to compare them.
	// This allows us to determine whether a file is already
	// formatted, without modifying the file.
	src, err := source.ReadAll(file.Filename, file.Source)
	if err != nil {
		return false, err
	}

	parseOpts := []parser.Option{parser.ParseComments, parser.Version(version)}
	syntax, err := parser.ParseFile(file.Filename, src, parseOpts...)
	if err != nil {
		return false, err
	}
	// Files read without the loader must follow its rule for @version too.
	if err := checkVersionAttr(file.Filename, syntax); err != nil {
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

	path, err := filepath.Rel(rootWorkingDir(), file.Filename)
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
