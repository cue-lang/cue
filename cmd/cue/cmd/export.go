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
	"encoding/json"
	"fmt"
	"io"

	"cuelang.org/go/cue"
	"github.com/spf13/cobra"
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

	# all files within the "mypkg" package: this includes all files in the
	# current directory and its ancestor directories that are marked with the
	# same package.
	cue export -p cloud

	# the -p flag can be omitted if the directory only contains files for
	# the "mypkg" package.
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
		"a": 1,
		"b", "my string"
	}

In absence of arguments, the current directory is loaded as a package instance.
A package instance for a directory contains all files in the directory and its
ancestor directories, up to the module root, belonging to the same package.
If the package is not explicitly defined by the '-p' flag, it must be uniquely
defined by the files in the current directory.


Formats
The following formats are recognized:

json    output as JSON
		Outputs any CUE value.

text    output as raw text
        The evaluated value must be of type string.
`,

		RunE: mkRunE(c, runExport),
	}
	flagMedia.Add(cmd)
	cmd.Flags().Bool(string(flagEscape), false, "use HTML escaping")

	return cmd
}

func runExport(cmd *Command, args []string) error {
	instances := buildFromArgs(cmd, args)
	w := cmd.OutOrStdout()

	for _, inst := range instances {
		root := inst.Value()
		switch media := flagMedia.String(cmd); media {
		case "json":
			err := outputJSON(cmd, w, root)
			exitIfErr(cmd, inst, err, true)
		case "text":
			err := outputText(w, root)
			exitIfErr(cmd, inst, err, true)
		default:
			return fmt.Errorf("export: unknown format %q", media)
		}
	}
	return nil
}

func outputJSON(cmd *Command, w io.Writer, v cue.Value) error {
	e := json.NewEncoder(w)
	e.SetIndent("", "    ")
	e.SetEscapeHTML(flagEscape.Bool(cmd))

	err := e.Encode(v)
	if err != nil {
		if x, ok := err.(*json.MarshalerError); ok {
			err = x.Err
		}
		fmt.Fprintln(w, err)
	}
	return nil
}

func outputText(w io.Writer, v cue.Value) error {
	str, err := v.String()
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(w, str)
	return err
}
