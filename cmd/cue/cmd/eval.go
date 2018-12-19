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
	"io"
	"strings"
	"text/tabwriter"

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

		tw := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 1, ' ', 0)
		defer tw.Flush()
		for _, inst := range instances {
			// TODO: use ImportPath or some other sanitized path.
			fmt.Fprintf(cmd.OutOrStdout(), "// %s\n", inst.Dir)
			p := evalPrinter{w: tw}
			if exprs == nil {
				p.print(inst.Value())
				fmt.Fprintln(tw)
			}
			for _, e := range exprs {
				p.print(inst.Eval(e))
				fmt.Fprintln(tw)
			}
		}
		return nil
	},
}

func init() {
	RootCmd.AddCommand(evalCmd)

	expressions = evalCmd.Flags().StringArrayP("expression", "e", nil, "evaluate this expression only")

}

var (
	expressions *[]string
)

type evalPrinter struct {
	w        io.Writer
	fset     *token.FileSet
	indent   int
	newline  bool
	formfeed bool
}

type ws byte

const (
	unindent    = -1
	indent      = 1
	newline  ws = '\n'
	vtab     ws = '\v'
	space    ws = ' '

	// maxDiffLen is the maximum different in length for object keys for which
	// to still align keys and values.
	maxDiffLen = 5
)

func (p *evalPrinter) print(args ...interface{}) {
	for _, a := range args {
		if d, ok := a.(int); ok {
			p.indent += d
			continue
		}
		if p.newline {
			nl := '\n'
			if p.formfeed {
				nl = '\f'
			}
			p.w.Write([]byte{byte(nl)})
			fmt.Fprint(p.w, strings.Repeat("    ", int(p.indent)))
			p.newline = false
		}
		switch v := a.(type) {
		case ws:
			switch v {
			case newline:
				p.newline = true
			default:
				p.w.Write([]byte{byte(v)})
			}
		case string:
			fmt.Fprint(p.w, v)
		case cue.Value:
			switch v.Kind() {
			case cue.StructKind:
				iter, err := v.AllFields()
				must(err)
				lastLen := 0
				p.print("{", indent, newline)
				for iter.Next() {
					value := iter.Value()
					key := iter.Label()
					newLen := len([]rune(key)) // TODO: measure cluster length.
					if lastLen > 0 && abs(lastLen, newLen) > maxDiffLen {
						p.formfeed = true
					} else {
						k := value.Kind()
						p.formfeed = k == cue.StructKind || k == cue.ListKind
					}
					p.print(key, ":", vtab, value, newline)
					p.formfeed = false
					lastLen = newLen
				}
				p.print(unindent, "}")
			case cue.ListKind:
				list, err := v.List()
				must(err)
				p.print("[", indent, newline)
				for list.Next() {
					p.print(list.Value(), newline)
				}
				p.print(unindent, "]")
			default:
				format.Node(p.w, v.Syntax())
			}
		}
	}
}

func abs(a, b int) int {
	a -= b
	if a < 0 {
		return -a
	}
	return a
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
