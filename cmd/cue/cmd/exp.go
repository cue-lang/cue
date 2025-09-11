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
	"time"

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

	// Commands to some day promote out of `cue exp`.
	cmd.AddCommand(newExpGenGoTypesCmd(c))

	// Hidden commands which are only meant for integration tests.
	cmd.AddCommand(&cobra.Command{
		// Hang forever, disregarding context cancellation when SIGINT is received.
		// Used to test that cmd/cue still exits in such a scenario.
		Use:    "internal-hang",
		Hidden: true,
		Run: func(cmd *cobra.Command, args []string) {
			// We don't do e.g. an empty select, as that can cause the runtime
			// to panic due to the detected deadlock.
			time.Sleep(time.Hour)
		},
	})
	return cmd
}

func newExpGenGoTypesCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "gengotypes",
		Short: "generate Go types from CUE definitions",
		Long: `
WARNING: THIS COMMAND IS EXPERIMENTAL.

gengotypes generates Go type definitions from exported CUE definitions.

The generated Go types are guaranteed to accept any value accepted by the CUE definitions,
but may be more general. For example, "string | int" will translate into the Go
type "any" because the Go type system is not able to express disjunctions.

To ensure that the resulting Go code works, any imported CUE packages or
referenced CUE definitions are transitively generated as well.
Code is generated in each CUE package directory at cue_types_${pkgname}_gen.go,
where the package name is omitted from the filename if it is implied by the import path.

Generated Go type and field names may differ from the original CUE names by default.
For instance, an exported definition "#foo" becomes "Foo",
and a nested definition like "#foo.#bar" becomes "Foo_Bar".

@go attributes can be used to override which name to be generated:

	package foo
	@go(betterpkgname)

	#Bar: {
		@go(BetterBarTypeName)
		renamed: int @go(BetterFieldName)
	}

The attribute "@go(-)" can be used to ignore a definition or field:

	#ignoredDefinition: {
		@go(-)
	}
	ignoredField: int @go(-)

"type=" overrides an entire value to generate as a given Go type expression:

	retypedLocal:  [string]: int @go(,type=map[LocalType]int)
	retypedImport: [...string]   @go(,type=[]"foo.com/bar".ImportedType)

"optional=" controls how CUE optional fields are generated as Go fields.
The default is "zero", representing a missing field as the zero value.
"nillable" ensures the generated Go type can represent missing fields as nil.

	optionalDefault?:  int                         // generates as "int64"
	optionalNillable?: int @go(,optional=nillable) // generates as "*int64"
	nested: {
		@go(,optional=nillable) // set for all fields under this struct
	}
`[1:],
		RunE: mkRunE(c, runExpGenGoTypes),
	}

	return cmd
}

func runExpGenGoTypes(cmd *Command, args []string) error {
	insts := load.Instances(args, &load.Config{})
	return gotypes.Generate(cmd.ctx, insts...)
}
