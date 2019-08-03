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
	"fmt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
	"github.com/spf13/cobra"
)

// newEvalCmd creates a new eval command
func newEvalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "eval",
		Short: "evaluate and print a configuration",
		Long: `eval evaluates, validates, and prints a configuration.

Printing is skipped if validation fails.

The --expression flag is used to evaluate an expression within the
configuration file, instead of the entire configuration file itself.

Examples:

  $ cat <<EOF > foo.cue
  a: [ "a", "b", "c" ]
  EOF

  $ cue eval foo.cue -e a[0] -e a[2]
  "a"
  "c"
`,
		RunE: runEval,
	}

	cmd.Flags().StringArrayP(string(flagExpression), "e", nil, "evaluate this expression only")

	cmd.Flags().BoolP(string(flagConcrete), "c", false,
		"require the evaluation to be concrete")

	cmd.Flags().BoolP(string(flagHidden), "H", false,
		"display hidden attributes")

	cmd.Flags().BoolP(string(flagOptional), "O", false,
		"display hidden attributes")

	cmd.Flags().BoolP(string(flagAttributes), "l", false,
		"display field attributes")

	cmd.Flags().BoolP(string(flagAll), "a", false,
		"show optional and hidden fields")

	// TODO: Option to include comments in output.
	return cmd
}

const (
	flagConcrete   flagName = "concrete"
	flagHidden     flagName = "show-hidden"
	flagOptional   flagName = "show-optional"
	flagAttributes flagName = "attributes"
)

func runEval(cmd *cobra.Command, args []string) error {
	instances := buildFromArgs(cmd, args)

	var exprs []ast.Expr
	for _, e := range flagExpression.StringArray(cmd) {
		expr, err := parser.ParseExpr("<expression flag>", e)
		if err != nil {
			return err
		}
		exprs = append(exprs, expr)
	}

	w := cmd.OutOrStdout()

	for _, inst := range instances {
		// TODO: use ImportPath or some other sanitized path.
		if len(instances) > 1 {
			fmt.Fprintf(w, "\n// %s\n", inst.Dir)
		}
		syn := []cue.Option{
			cue.Attributes(flagAttributes.Bool(cmd)),
			cue.Optional(flagAll.Bool(cmd) || flagOptional.Bool(cmd)),
		}
		if flagConcrete.Bool(cmd) {
			syn = append(syn, cue.Concrete(true))
		}
		if flagHidden.Bool(cmd) || flagAll.Bool(cmd) {
			syn = append(syn, cue.Hidden(true))
		}
		opts := []format.Option{
			format.UseSpaces(4),
			format.TabIndent(false),
		}
		if flagSimplify.Bool(cmd) {
			opts = append(opts, format.Simplify())
		}

		if exprs == nil {
			v := inst.Value()
			if flagConcrete.Bool(cmd) && !flagIgnore.Bool(cmd) {
				if err := v.Validate(cue.Concrete(true)); err != nil {
					exitIfErr(cmd, inst, err, false)
					continue
				}
			}
			b, _ := format.Node(getSyntax(v, syn), opts...)
			_, _ = w.Write(b)
		}
		for _, e := range exprs {
			if len(exprs) > 1 {
				fmt.Fprint(w, "// ")
				b, _ := format.Node(e)
				_, _ = w.Write(b)
				fmt.Fprintln(w)
			}
			v := inst.Eval(e)
			if flagConcrete.Bool(cmd) && !flagIgnore.Bool(cmd) {
				if err := v.Validate(cue.Concrete(true)); err != nil {
					exitIfErr(cmd, inst, err, false)
					continue
				}
			}
			b, _ := format.Node(getSyntax(v, syn), opts...)
			_, _ = w.Write(b)
			fmt.Fprintln(w)
		}
	}
	return nil
}

func getSyntax(v cue.Value, opts []cue.Option) ast.Node {
	n := v.Syntax(opts...)
	switch x := n.(type) {
	case *ast.StructLit:
		n = &ast.File{Decls: x.Elts}
	}
	return n
}
