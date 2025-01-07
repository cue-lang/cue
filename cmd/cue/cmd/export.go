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
		Long: `export evaluates the configuration found in the current
directory and prints the emit value to stdout.

Examples:
Evaluated and emit

	# a single file
	cue export config.cue

	# multiple files: these are combined at the top-level. Order doesn't matter.
	cue export file1.cue foo/file2.cue

	# all files within the "cloud" package, including all files in the
	# current directory and its ancestor directories that are marked with the
	# same package, up to the root of the containing module.
	cue export .:cloud

	# the package name can be omitted if the directory only contains files for
	# the "cloud" package.
	cue export

Emit value:
For CUE files, the generated configuration is derived from the top-level
single expression, the emit value. For example, the file

	// config.cue
	arg1: 1
	arg2: "my string"

	{
		a: arg1
		b: arg2
	}

yields the following JSON:

	{
		"arg1": 1,
		"a": 1,
		"arg2": "my string",
		"b": "my string"
	}

In absence of arguments, the current directory is loaded as a package instance.
A package instance for a directory contains all files in the directory and its
ancestor directories, up to the module root, belonging to the same package. If
a single package is not uniquely defined by the files in the current directory
then the package name must be specified as an explicit argument using
".:<package-name>" syntax.


Formats

The following formats are recognized:

    cue  output as CUE
              Outputs any CUE value.

   json  output as JSON
              Outputs any CUE value.

   yaml  output as YAML
              Outputs any CUE value.

   text  output as raw text
              The evaluated value must be of type string.

 binary  output as raw binary
              The evaluated value must be of type string or bytes.
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
