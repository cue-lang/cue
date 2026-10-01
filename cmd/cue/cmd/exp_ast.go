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
	"path/filepath"
	"slices"
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/build"
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
	cfg := astinternal.DebugConfig{
		OmitEmpty:       flagOmitEmpty.Bool(cmd),
		IncludeNodeRefs: flagRefs.Bool(cmd),
		AllPositions:    flagPos.Bool(cmd),
		Filename:        relativeFilename,
	}
	w := cmd.OutOrStdout()
	printFile := func(file *ast.File) error {
		_, err := w.Write(astinternal.AppendDebug(nil, file, cfg))
		return err
	}
	if flagFiles.Bool(cmd) {
		var errs errors.Error
		for _, name := range args {
			var src any // nil reads the named file
			if name == "-" {
				src = cmd.InOrStdin()
			}
			file, err := parser.ParseFile(name, src, parser.ParseComments)
			if file == nil {
				return err // the source could not be read
			}
			if err != nil {
				errs = errors.Append(errs, errors.Promote(err, "parse error"))
			}
			// Even if there are errors, it can still be useful to
			// show a (possibly) partial AST.
			if err := printFile(file); err != nil {
				return err
			}
		}
		return errs
	}
	// TODO: should we produce output in txtar form for the sake of
	// more clearly separating the AST for each file?
	// [ast.File.Filename] already has the full filename,
	// but as one of the first fields it's not a great separator.
	for _, inst := range loadExpASTInstances(cmd, args) {
		if err := inst.Err; err != nil {
			return err
		}
		for _, file := range inst.Files {
			if err := printFile(file); err != nil {
				return err
			}
		}
	}
	return nil
}

func loadExpASTInstances(cmd *Command, args []string) []*build.Instance {
	return load.Instances(args, &load.Config{
		// cue exp ast only cares about syntax trees; loading the imports just causes
		// extra work and may lead to errors we don't care about.
		SkipImports: true,
		Stdin:       cmd.InOrStdin(),
	})
}

// relativeFilename makes absolute filenames relative to the working directory.
// Like the errors printed by the CLI, it uses forward slashes in tests.
func relativeFilename(name string) string {
	rel, err := filepath.Rel(rootWorkingDir(), name)
	if err != nil {
		return name // for example, a relative filename or "-"
	}
	if testing.Testing() {
		rel = filepath.ToSlash(rel)
	}
	return rel
}

func newExpASTJoinCmd(c *Command) *cobra.Command {
	// TODO: add a flag to drop comments, which is useful when reducing bug reproducers.
	return &cobra.Command{
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
}

func runExpASTJoin(cmd *Command, args []string) error {
	insts := loadExpASTInstances(cmd, args)
	if len(insts) != 1 {
		return errors.New("joining multiple instances is not possible yet")
	}
	inst := insts[0]
	if err := inst.Err; err != nil {
		return err
	}
	// TODO: we should sort and deduplicate imports.
	imports := &ast.ImportDecl{}
	joint := &ast.File{Decls: []ast.Decl{imports}}
	for _, file := range inst.Files {
		imports.Specs = slices.AppendSeq(imports.Specs, file.ImportSpecs())
		joint.Decls = append(joint.Decls, file.Decls[len(file.Preamble()):]...)
	}

	// Sanitize the resulting file so that, for example,
	// multiple packages imported as the same name avoid collisions.
	if err := astutil.SanitizeFiles([]*ast.File{joint}); err != nil {
		return err
	}
	out, err := format.Node(joint)
	if err != nil {
		return err
	}
	_, err = cmd.OutOrStdout().Write(out)
	return err
}
