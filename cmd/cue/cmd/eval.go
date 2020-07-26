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

	"github.com/spf13/cobra"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/encoding"
	"cuelang.org/go/internal/filetypes"
)

// newEvalCmd creates a new eval command
func newEvalCmd(c *Command) *cobra.Command {
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
		RunE: mkRunE(c, runEval),
	}

	addOutFlags(cmd.Flags(), true)
	addOrphanFlags(cmd.Flags())

	cmd.Flags().StringArrayP(string(flagExpression), "e", nil, "evaluate this expression only")

	cmd.Flags().BoolP(string(flagConcrete), "c", false,
		"require the evaluation to be concrete")

	cmd.Flags().BoolP(string(flagHidden), "H", false,
		"display hidden fields")

	cmd.Flags().BoolP(string(flagOptional), "O", false,
		"display optional fields")

	cmd.Flags().BoolP(string(flagAttributes), "A", false,
		"display field attributes")

	cmd.Flags().BoolP(string(flagAll), "a", false,
		"show optional and hidden fields")

	cmd.Flags().StringArrayP(string(flagInject), "t", nil,
		"set the value of a tagged field")

	// TODO: Option to include comments in output.
	return cmd
}

const (
	flagConcrete   flagName = "concrete"
	flagHidden     flagName = "show-hidden"
	flagOptional   flagName = "show-optional"
	flagAttributes flagName = "show-attributes"
)

func runEval(cmd *Command, args []string) error {
	b, err := parseArgs(cmd, args, &config{outMode: filetypes.Eval})
	exitOnErr(cmd, err, true)

	syn := []cue.Option{
		cue.Final(), // for backwards compatibility
		cue.Definitions(true),
		cue.Attributes(flagAttributes.Bool(cmd)),
		cue.Optional(flagAll.Bool(cmd) || flagOptional.Bool(cmd)),
	}

	// Keep for legacy reasons. Note that `cue eval` is to be deprecated by
	// `cue` eventually.
	opts := []format.Option{
		format.UseSpaces(4),
		format.TabIndent(false),
	}
	if flagSimplify.Bool(cmd) {
		opts = append(opts, format.Simplify())
	}
	b.encConfig.Format = opts

	e, err := encoding.NewEncoder(b.outFile, b.encConfig)
	exitOnErr(cmd, err, true)

	iter := b.instances()
	defer iter.close()
	for i := 0; iter.scan(); i++ {
		id := ""
		if len(b.insts) > 1 {
			id = iter.id()
		}
		v := iter.instance().Value()

		if flagConcrete.Bool(cmd) {
			syn = append(syn, cue.Concrete(true))
		}
		if flagHidden.Bool(cmd) || flagAll.Bool(cmd) {
			syn = append(syn, cue.Hidden(true))
		}

		errHeader := func() {
			if id != "" {
				fmt.Fprintf(cmd.OutOrStderr(), "// %s\n", id)
			}
		}

		if len(b.expressions) > 1 {
			b, _ := format.Node(b.expressions[i%len(b.expressions)])
			id = string(b)
		}
		if err := v.Err(); err != nil {
			errHeader()
			return err
		}
		if flagConcrete.Bool(cmd) && !flagIgnore.Bool(cmd) {
			if err := v.Validate(cue.Concrete(true)); err != nil {
				errHeader()
				exitOnErr(cmd, err, false)
				continue
			}
		}

		f := getSyntax(v, syn)
		f.Filename = id
		err := e.EncodeFile(f)
		if err != nil {
			errHeader()
			exitOnErr(cmd, err, false)
		}
	}
	exitOnErr(cmd, iter.err(), true)
	return nil
}

func getSyntax(v cue.Value, opts []cue.Option) *ast.File {
	return internal.ToFile(v.Syntax(opts...))
}
