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

By default values are emitted to standard output encoded as JSON. A different
destination can be selected using the --outfile/-o flag, and a different
encoding can be specified with the --out flag.


Configurations

When invoked without any arguments the command evaluates the CUE package in the
current directory. If more than one package is present in the current directory
then a package path argument must be provided.

Arguments can be CUE package paths, CUE files, non-CUE data files, or schema
files in supported encodings. The contents of all arguments are unified to form
the evaluated configuration unless multiple CUE package paths are mentioned.
If multiple CUE package paths are mentioned then individual files of any kind
cannot also be mentioned, and values are emitted from each package path.
This unification has the implict effect of validating data in the unified
configuration against all constraints that are present.

Packages are loaded as package instances. A package instance is the unification
of all CUE files with the same package name found in the package's directory and
every ancestor directory leading up to the module root.


Emitted expressions

The default expression whose value is emitted is the top-level of the evaluated
configuration. A different expression can be specified with the --expression/-e
flag, which can be repeated to emit the values of multiple expressions.

The export command reports an error if the value of any expression to be emitted
is incomplete - that is, if it contains any non-concrete values that cannot be
represented in data-only encodings such as JSON.


Package paths

Package paths must be either absolute, or relative. Absolute package paths
identify a specific CUE package within a module. The module may be the
current module or one of its dependencies.

Relative package paths start with "." or ".." and are resolved as if they were
filesystem paths, relative to the current directory. If a package's name does
not match the directory in which it is stored then its name must be provided as
a suffix, separated from the package path with a ":" - as in "./foo/bar:baz".


Supported encodings

The following encodings are recognized by the --out flag:

    "cue":    output as CUE    (can encode any value)
    "json":   output as JSON   (can encode any value)
    "toml":   output as TOML   (can encode any value)
    "yaml":   output as YAML   (can encode any value)
    "text":   output as text   (can only encode values of type string)
    "binary": output as binary (can only encode values of type string or bytes)

See "cue help filetypes" for more information on values accepted by --out.


Examples

- Export the contents of the only CUE package in the current directory as JSON:
  $ cue export

- Export the contents of an absolute package path as YAML:
  $ cue export cue.example/foo/bar --out yaml

- Export the contents of one of many CUE packages in the current directory
  unified with a YAML file, emitting an expression other than the top-level as
  JSON:
  $ cue export .:example path/to/data.yml --expression aKey

- Export the contents of one of many CUE packages in a different, relative
  directory as TOML:
  $ cue export ./relative/path/to/directory:example --out toml

- Export the unified contents of multiple CUE files as CUE:
  $ cue export config.cue dir/extraData.cue --out cue

- Unify the contents of a CUE package and a TOML file, and emit the JSON
  encoding of multiple expressions rather than the top-level of the evaluation:
  $ cue export cue.example/some/package data.toml -e key1 -e key2


More help

An in-depth guide to the "cue export" command:
    https://cuelang.org/docs/concept/using-the-cue-export-command/
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
