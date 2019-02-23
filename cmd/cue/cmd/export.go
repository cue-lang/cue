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

	"github.com/spf13/cobra"
)

// exportCmd represents the emit command
var exportCmd = &cobra.Command{
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
`,

	RunE: func(cmd *cobra.Command, args []string) error {
		instances := buildFromArgs(cmd, args)
		w := cmd.OutOrStdout()
		e := json.NewEncoder(w)
		e.SetIndent("", "    ")
		e.SetEscapeHTML(*escape)

		root := instances[0].Value()
		err := e.Encode(root)
		if err != nil {
			fmt.Fprintln(w, err)
		}
		return nil
	},
}

var (
	escape *bool
)

func init() {
	rootCmd.AddCommand(exportCmd)

	exportCmd.Flags().StringP("output", "o", "json", "output format (json only for now)")
	escape = exportCmd.Flags().BoolP("escape", "e", false, "use HTML escaping")
}
