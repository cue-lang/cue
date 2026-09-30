// Copyright 2026 CUE Authors
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
	"fmt"
	"io"
	"os"
	"slices"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/astinternal"

	"github.com/spf13/cobra"
)

func newExpASTCmd(c *Command) *cobra.Command {
	cmd := commandGroup(&cobra.Command{
		Use:   "ast <cmd> [arguments]",
		Short: "inspect and manipulate CUE syntax trees",
		Long: `
WARNING: THIS COMMAND IS EXPERIMENTAL.

ast groups commands which work on CUE syntax trees,
mainly to help debug the parser and to reduce bug reproducers.
`[1:],
	})
	cmd.AddCommand(newExpASTPrintCmd(c))
	cmd.AddCommand(newExpASTJoinCmd(c))
	return cmd
}

func newExpASTPrintCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "print [flags] [inputs]",
		Short: "print CUE syntax trees",
		Long: `
print writes multi-line Go-like representations of CUE syntax trees.

Arguments are loaded as packages; see 'cue help inputs'.
Use --files to parse each argument as a single file instead,
where "-" reads from stdin. In this mode, a file with syntax errors
still has its partial syntax tree printed.
`[1:],
		RunE: mkRunE(c, runExpASTPrint),
	}
	f := cmd.Flags()
	f.Bool(string(flagOmitEmpty), false,
		"omit empty strings, empty structs, empty lists, nil pointers, invalid positions, and missing tokens")
	f.Bool(string(flagRefs), false, "print node references for Node fields")
	f.Bool(string(flagFiles), false, "treat arguments as literal files; do not try to load as package")
	f.Bool(string(flagPos), false, "print position info for all nodes")
	// Note that DebugConfig also has a Filter func, but that doesn't lend itself well
	// to a command line flag. Perhaps we could provide some commonly used filters,
	// such as "positions only" or "skip positions".
	return cmd
}

func runExpASTPrint(cmd *Command, args []string) error {
	lcfg := &load.Config{
		// cue exp ast only cares about syntax trees; loading the imports just causes
		// extra work and may lead to errors we don't care about.
		SkipImports: true,
		Stdin:       cmd.InOrStdin(),
	}
	cfg := astinternal.DebugConfig{
		OmitEmpty:       flagOmitEmpty.Bool(cmd),
		IncludeNodeRefs: flagRefs.Bool(cmd),
		AllPositions:    flagPos.Bool(cmd),
	}
	if flagFiles.Bool(cmd) {
		for _, f := range args {
			var data []byte
			var err error
			if f == "-" {
				data, err = io.ReadAll(cmd.InOrStdin())
			} else {
				data, err = os.ReadFile(f)
			}
			if err != nil {
				return err
			}
			astf, err := parser.ParseFile(f, data, parser.ParseComments)
			if err != nil {
				fmt.Fprint(cmd.Stderr(), errors.Details(err, nil))
			}
			if astf != nil {
				// Even if there are errors, it can still be useful to
				// show a (possibly) partial AST.
				out := astinternal.AppendDebug(nil, astf, cfg)
				cmd.OutOrStdout().Write(out)
			}
		}
		return nil
	}
	// TODO: should we produce output in txtar form for the sake of
	// more clearly separating the AST for each file?
	// [ast.File.Filename] already has the full filename,
	// but as one of the first fields it's not a great separator.
	insts := load.Instances(args, lcfg)
	for _, inst := range insts {
		if err := inst.Err; err != nil {
			return err
		}
		for _, file := range inst.Files {
			out := astinternal.AppendDebug(nil, file, cfg)
			cmd.OutOrStdout().Write(out)
		}
	}
	return nil
}

func newExpASTJoinCmd(c *Command) *cobra.Command {
	// TODO: add a flag drop comments, which is useful when reducing bug reproducers.
	cmd := &cobra.Command{
		Use:   "join [flags] [inputs]",
		Short: "join a package into a single file",
		Long: `
join writes the files of the input package as a single CUE file,
which is useful to reduce bug reproducers.
Joining multiple packages is not supported yet.

See 'cue help inputs' as well.
`[1:],
		RunE: mkRunE(c, runExpASTJoin),
	}
	return cmd
}

func runExpASTJoin(cmd *Command, args []string) error {
	lcfg := &load.Config{
		// cue exp ast only cares about syntax trees; loading the imports just causes
		// extra work and may lead to errors we don't care about.
		SkipImports: true,
		Stdin:       cmd.InOrStdin(),
	}
	var jointImports []*ast.ImportSpec
	var jointFields []ast.Decl
	insts := load.Instances(args, lcfg)
	if len(insts) != 1 {
		return errors.New("joining multiple instances is not possible yet")
	}
	inst := insts[0]
	if err := inst.Err; err != nil {
		return err
	}
	for _, file := range inst.Files {
		jointImports = slices.Concat(jointImports, slices.Collect(file.ImportSpecs()))

		fields := file.Decls[len(file.Preamble()):]
		jointFields = slices.Concat(jointFields, fields)
	}
	// TODO: we should sort and deduplicate imports.
	joint := &ast.File{Decls: slices.Concat([]ast.Decl{
		&ast.ImportDecl{Specs: jointImports},
	}, jointFields)}

	// Sanitize the resulting file so that, for example,
	// multiple packages imported as the same name avoid collisions.
	if err := astutil.Sanitize(joint); err != nil {
		return err
	}

	out, err := format.Node(joint)
	if err != nil {
		return err
	}
	cmd.OutOrStdout().Write(out)
	return nil
}
