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
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/tools/fix"
	"github.com/spf13/cobra"
)

// fixCommentsHelp tells the user of a command which runs the rewrites what
// the comments they leave behind ask of them.
var fixCommentsHelp = `
A TODO(cue-fix) comment marks a construct whose rewritten form does not
mean what it did: the rewrite could not keep its behavior, and someone
must decide what the file should say. A NOTE(cue-fix) comment marks a
rewrite which keeps the behavior but may do more than needed, such as a
spread on an embedded reference the rewrite cannot see to be open or
closed; the file is correct as written, and the NOTE says what can go.
`[1:]

func newFixCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fix [flags] [packages]",
		Short: "rewrite packages to latest standards",
		Long: `Fix finds CUE programs that use old syntax and old APIs and rewrites them to use newer ones.
After you update to a new CUE release, fix helps make the necessary changes
to your program.

Without any packages, fix applies to every file of the module, from
whichever directory it is run: the files of the module's own tree, the
packages ./... denotes from the module root and _ and testdata
directories included, and every CUE file under cue.mod/gen, cue.mod/pkg
and cue.mod/usr, found by walking the module and rewritten one package at
a time without loading, so a package whose import cannot be resolved is
rewritten all the same. Symbolic links are handled as cue mod upgrade
handles them. A file the parser rejects at the module's language version
is skipped and reported. With packages named, the loader selects the
files, and its errors are reported.

A file that cannot be rewritten stops fix from writing anything, unless
-f is given, in which case the files that converted are written; either
way the errors are printed and the exit status is non-zero.


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

` + fixCommentsHelp,
		RunE: mkRunE(c, runFixAll),
	}

	cmd.Flags().BoolP(string(flagForce), "f", false,
		"rewrite even when there are errors")

	cmd.Flags().StringSlice("exp", nil,
		"list of experiments to port")

	cmd.Flags().Bool("remove-list-commas", false,
		"remove commas from multiline list elements (v0.17.0+)")
	addCUEOutputFlags(cmd)
	addLanguageVersionFlag(cmd)

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

	if len(args) == 0 {
		return fixModule(cmd, flagForce.Bool(cmd), opts...)
	}
	_, errs := fixInstances(cmd, args, flagForce.Bool(cmd), opts...)
	return errs
}

// fixModule rewrites every file of the module, selected and grouped as
// cue mod upgrade selects and groups them, at the module's own language
// version. Without force a failed rewrite leaves every file unwritten.
func fixModule(cmd *Command, force bool, opts ...fix.Option) error {
	// The module's files are at its own version, so --language-version,
	// a fallback for files outside any module, never applies here.
	if _, err := languageVersionFlag(cmd.Flags()); err != nil {
		return err
	}
	root, err := findModuleRoot()
	if err != nil {
		return err
	}
	mf, _, version, err := parseModuleFile(root)
	if err != nil {
		return err
	}
	results, runErr := rewriteModule(root, mf, version, version, opts...)
	stderr := cmd.ErrOrStderr()
	_, failed := reportRewrites(stderr, results)
	if runErr != nil {
		return runErr
	}
	if failed > 0 && !force {
		return ErrPrintedError
	}
	if n := writeRewrites(stderr, results); n > 0 || failed > 0 {
		return ErrPrintedError
	}
	return nil
}

// fixInstances loads the packages args name and rewrites their files; it
// is the explicit-argument path of cue fix. Without arguments, fix and mod
// upgrade select the module's files by walking it instead (fixModule).
func fixInstances(cmd *Command, args []string, force bool, opts ...fix.Option) ([]*build.Instance, errors.Error) {
	langVersion, err := languageVersionFlag(cmd.Flags())
	if err != nil {
		return nil, errors.Promote(err, "")
	}
	instances := load.Instances(args, &load.Config{
		Tests:           true,
		Tools:           true,
		Package:         "*",
		LanguageVersion: langVersion,
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

	// With force the errors were proceeded past, not resolved: they are
	// still reported, and still make the command fail.
	return instances, errs
}
