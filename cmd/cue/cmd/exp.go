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
	"cuelang.org/go/cue/load"
	"cuelang.org/go/internal/encoding/gotypes"

	"github.com/spf13/cobra"
)

func newExpCmd(c *Command) *cobra.Command {
	cmd := commandGroup(&cobra.Command{
		// Experimental commands are hidden by design.
		Hidden: true,

		Use:   "exp <cmd> [arguments]",
		Short: "experimental commands",
		Long: `
exp groups commands which are still in an experimental stage.

Experimental commands may be changed or removed at any time,
as the objective is to gain experience and then move the feature elsewhere.
`[1:],
	})

	cmd.AddCommand(newExpGenGoTypesCmd(c))
	return cmd
}

// TODO(mvdan): document the "optional" attribute option when finished.

func newExpGenGoTypesCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gengotypes",
		Short: "generate Go types from CUE definitions",
		Long: `
gengotypes generates Go type definitions from exported CUE definitions.

*This command is experimental and may be changed at any time - see "cue help exp"*

The generated Go types are guaranteed to accept any value accepted by the CUE definitions,
but may be more general. For example, "string | int" will translate into the Go
type "any" because the Go type system is not able to express
disjunctions.

To ensure that the resulting Go code works, any imported CUE packages or
referenced CUE definitions are transitively generated as well.
The generated code is placed in cue_types*_gen.go files in the directory of
each CUE package.

Generated Go type and field names may differ from the original CUE names by default.
For instance, an exported definition "#foo" becomes "Foo",
given that Go uses capitalization to export names in a package,
and a nested definition like "#foo.#bar" becomes "Foo_Bar",
given that Go does not allow declaring nested types.

@go attributes can be used to override which name or type to be generated, for example:

	package foo
	@go(betterpkgname)

	#Bar: {
		@go(BetterBarTypeName)
		renamed: int @go(BetterFieldName)

		retypedLocal:  [...string] @go(,type=[]LocalType)
		retypedImport: [...string] @go(,type=[]"foo.com/bar".ImportedType)
	}

The attribute "@go(-)" can be used to ignore a definition or field, for example:

	#ignoredDefinition: {
		@go(-)
	}
	ignoredField: int @go(-)
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
