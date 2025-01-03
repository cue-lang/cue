// Copyright 2024 CUE Authors
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

	"cuelang.org/go/cue/load"
	"cuelang.org/go/internal/encoding/gotypes"

	"github.com/spf13/cobra"
)

func newExpCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		// Experimental commands are hidden by design.
		Hidden: true,

		Use:   "exp <cmd> [arguments]",
		Short: "experimental commands",
		Long: `
exp groups commands which are still in an experimental stage.

Experimental commands may be changed or removed at any time,
as the objective is to gain experience and then move the feature elsewhere.
`[1:],
		RunE: mkRunE(c, func(cmd *Command, args []string) error {
			stderr := cmd.Stderr()
			if len(args) == 0 {
				fmt.Fprintln(stderr, "exp must be run as one of its subcommands")
			} else {
				fmt.Fprintf(stderr, "exp must be run as one of its subcommands: unknown subcommand %q\n", args[0])
			}
			fmt.Fprintln(stderr, "Run 'cue help exp' for known subcommands.")
			return ErrPrintedError
		}),
	}

	cmd.AddCommand(newExpGenGoTypesCmd(c))
	return cmd
}

func newExpGenGoTypesCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gengotypes",
		Short: "generate Go types from CUE definitions",
		Long: `
gengotypes generates Go type definitions from exported CUE definitions.

The generated Go types are guaranteed to accept any value accepted by the CUE definitions,
but may be more general. For example, "string | int" will translate into the Go
type "any" because the Go type system is not able to express
disjunctions.

To ensure that the resulting Go code works, any imported CUE packages or
referenced CUE definitions are transitively generated as well.
The generated code is placed in cue_gen.go files in the directory of each CUE package.

Generated Go type and field names may differ from the original CUE names by default.
For instance, an exported definition "#foo" becomes "Foo",
given that Go uses capitalization to export names in a package,
and a nested definition like "#foo.#bar" becomes "Foo_Bar",
given that Go does not allow declaring nested types.

@go attributes can be used to override which name or type to be generated, for example:

    renamed: int @go(BetterName)
    retyped: string @go(,type=foo.com/bar.NamedString)

TODO: support @go(,generate=true) to force a type to be generated or skipped

`[1:],
		// TODO: write a long help text once the feature set is reasonably stable.
		RunE: mkRunE(c, runExpGenGoTypes),
	}

	return cmd
}

func runExpGenGoTypes(cmd *Command, args []string) error {
	insts := load.Instances(args, &load.Config{})
	return gotypes.Generate(cmd.ctx, insts...)
}
