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
	"github.com/spf13/cobra"

	"cuelang.org/go/internal/encoding"
	"cuelang.org/go/internal/filetypes"
)

// newExportCmd creates and export command
func newExportCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export",
		Short: "output data in a standard format",
		Long: `
The export command evaluates a configuration and emits the value of one or more
expressions.

## Inputs

When invoked without any arguments the command evaluates the CUE package in the
current directory. If more than one package is present in the current directory
then an input argument must be provided.

Input arguments can be CUE packages, CUE files, non-CUE files, or some
combinations of those. See "cue help inputs" for more detail.

## Output

By default the top-level of the evaluation is emitted to standard output,
encoded as JSON. A different destination can be specified using the
--outfile/-o flag. An alternative encoding can be selected with the --out flag.
One or more different expressions can be emitted using the --expression/-e flag.

The command reports an error if the value of any expression to be emitted is
incomplete - that is, if it contains any non-concrete values that cannot be
represented in data-only encodings such as JSON.

The following encodings are recognized by the --out flag:

    cue        Output as CUE    (can encode any value)
    json       Output as JSON   (can encode any value)
    toml       Output as TOML   (can encode any value)
    yaml       Output as YAML   (can encode any value)
    text       Output as text   (can only encode values of type string)
    binary     Output as binary (can only encode values of type string or bytes)

See "cue help filetypes" for more information on values accepted by --out.

## Examples

- Export the contents of the only CUE package in the current directory as JSON:
  $ cue export

- Export the contents of an absolute package path as YAML:
  $ cue export cue.example/foo/bar --out yaml

- Unify the contents of the "example" package (which exists alongside other
  package in the current directory) with a YAML file, emitting the value of the
  "aKey" field as JSON:
  $ cue export .:example path/to/data.yml --expression aKey

- Export the contents of one of many CUE packages in a different, relative
  directory as TOML:
  $ cue export ./relative/path/to/directory:example --out toml

- Export the unified contents of multiple CUE files as CUE:
  $ cue export config.cue dir/extraData.cue --out cue

- Unify the contents of a CUE package and a TOML file, emittting the values of
  multiple expressions (rather than the top-level of the evaluation) as JSON:
  $ cue export cue.example/some/package data.toml -e key1 -e key2

## More help

- An in-depth guide to the "cue export" command:
    https://cuelang.org/docs/concept/using-the-cue-export-command/
- The "cue help inputs" command:
    https://cuelang.org/docs/reference/command/cue-help-inputs/
- The "cue help filetypes" command:
    https://cuelang.org/docs/reference/command/cue-help-filetypes/
`[1:],
		// TODO: some formats are missing for sure, like "jsonl" or "textproto" from internal/filetypes/types.cue.
		RunE: mkRunE(c, runExport),
	}

	addOutFlags(cmd.Flags(), true)
	addOrphanFlags(cmd.Flags())
	addInjectionFlags(cmd.Flags(), false, false)

	cmd.Flags().Bool(string(flagEscape), false, "use HTML escaping")
	cmd.Flags().StringArrayP(string(flagExpression), "e", nil, "export this expression only")

	return cmd
}

func runExport(cmd *Command, args []string) error {
	b, err := parseArgs(cmd, args, &config{mode: filetypes.Export})
	if err != nil {
		return err
	}

	enc, err := encoding.NewEncoder(cmd.ctx, b.outFile, b.encConfig)
	if err != nil {
		return err
	}

	iter := b.instances()
	defer iter.close()
	for iter.scan() {
		v := iter.value()
		err := enc.Encode(v)
		if err != nil {
			return err
		}
	}
	if err := iter.err(); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	return nil
}
