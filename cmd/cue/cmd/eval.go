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
	"cuelang.org/go/cue/token"
	"github.com/spf13/cobra"
)

// evalCmd represents the eval command
var evalCmd = &cobra.Command{
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
	RunE: func(cmd *cobra.Command, args []string) error {
		instances := buildFromArgs(cmd, args)

		var exprs []ast.Expr
		for _, e := range *expressions {
			expr, err := parser.ParseExpr(token.NewFileSet(), "<expression flag>", e)
			if err != nil {
				return err
			}
			exprs = append(exprs, expr)
		}

		w := cmd.OutOrStdout()

		for _, inst := range instances {
			// TODO: use ImportPath or some other sanitized path.
			fmt.Fprintf(w, "// %s\n", inst.Dir)
			syn := []cue.Option{
				cue.Attributes(*attrs),
				cue.Optional(*all || *optional),
			}
			if *compile {
				syn = append(syn, cue.RequireConcrete())
			}
			if *hidden || *all {
				syn = append(syn, cue.Hidden(true))
			}
			opts := []format.Option{
				format.UseSpaces(4),
				format.TabIndent(false),
			}
			if exprs == nil {
				v := inst.Value()
				if *compile {
					err := v.Validate(cue.RequireConcrete())
					exitIfErr(cmd, inst, err, false)
					continue
				}
				format.Node(w, v.Syntax(syn...), opts...)
				fmt.Fprintln(w)
			}
			for _, e := range exprs {
				format.Node(w, inst.Eval(e).Syntax(syn...), opts...)
				fmt.Fprintln(w)
			}
		}
		return nil
	},
}

func init() {
	rootCmd.AddCommand(evalCmd)

	expressions = evalCmd.Flags().StringArrayP("expression", "e", nil, "evaluate this expression only")

	compile = evalCmd.Flags().BoolP("concrete", "c", false,
		"require the evaluation to be concrete")

	hidden = evalCmd.Flags().BoolP("show-hidden", "H", false,
		"display hidden attributes")

	optional = evalCmd.Flags().BoolP("show-optional", "O", false,
		"display hidden attributes")

	attrs = evalCmd.Flags().BoolP("attributes", "l", false,
		"display field attributes")

	all = evalCmd.Flags().BoolP("all", "a", false,
		"show optional and hidden fields")

	// TODO: Option to include comments in output.
}

var (
	expressions *[]string
	compile     *bool
	attrs       *bool
	all         *bool
	hidden      *bool
	optional    *bool
)
