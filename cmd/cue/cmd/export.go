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
		Long: `The export command evaluates a configuration, and emits the value of one or more
expressions. By default, values are emitted to standard output as JSON. A
different destination can be selected with the --outfile flag, and a different
encoding can be selected with the --out flag.

## Configurations

When invoked without any arguments, the evaluated configuration is the CUE
package in the current directory. If more than one package is present in the
current directory then a package path must be provided.

Arguments must be CUE package paths, CUE files, or non-CUE data files or schema
files in supported encodings. The contents of all arguments are unified to form
the evaluated configuration, except when multiple CUE package paths are
provided. If multiple CUE package paths are provided then individual files must
not be provided and a separate value is emitted from each package path provided.

Unification has the implict effect of validating data in the unified
configuration against any constraints that are present.

## Emitted expressions

The default expression to have its value emitted is the top-level of the
evaluated configuration. A different expression can be selected with the -e
flag. The flag can be repeated to emit the values of multiple expressions.

## Package paths

Package paths must be either absolute, or relative. Absolute package paths
identify a specific CUE package within a CUE module. The module may be the
current module, or it may be a module required by the current module.

Relative package paths start with "." or "..". They are resolved as if they were
filesystem paths, relative to the current directory. If a package's name does
not match the directory in which it is stored, its name must be provided as a
suffix, separated from the package path with a ":".

## Supported encodings

The following encodings are recognized by the --out flag:

    "cue":    output as CUE    (can encode any value)
    "json":   output as JSON   (can encode any value)
    "yaml":   output as YAML   (can encode any value)
    "text":   output as text   (can only encode values of type string)
    "binary": output as binary (can only encode values of type string or bytes)

See "cue help filetypes" for more information on values accepted by --out.

## Examples

- Export the contents of the only CUE package in the current directory as JSON:
  $ cue export

- Export the contents of one of many CUE packages in the current directory
  unified with a YAML file emitting an expression other than the top-level as
  JSON:
  $ cue export .:example path/to/data.yml -e aKey

- Export the contents of one of many CUE packages in a different, relative
  directory as YAML:
  $ cue export ./relative/path/to/directory:example --out yaml

- Export the unified contents of multiple CUE files as CUE:
  $ cue export config.cue dir/extraData.cue --out cue

- Export the unified contents of a CUE package and a JSON file, emitting
  multiple expression other than the top-level as JSON:
  $ cue export cue.example/some/package data.json -e key1 -e key2
`,
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
	b, err := parseArgs(cmd, args, &config{outMode: filetypes.Export})
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
