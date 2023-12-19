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
	"os"

	"github.com/spf13/cobra"
)

func newGetCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <language> [packages]",
		Short: "add dependencies to the current module",
		Long: `Get downloads packages or modules for CUE or another language
to include them in the module's pkg directory.

Get requires an additional language field to determine for which
language definitions should be fetched. If get fetches definitions
for a language other than CUE, the definitions are extracted from
the source of the respective language and stored.
The specifics on how dependencies are fechted and converted vary
per language and are documented in the respective subcommands.
`,
		RunE: mkRunE(c, func(cmd *Command, args []string) error {
			stderr := cmd.Stderr()
			if len(args) == 0 {
				fmt.Fprintln(stderr, "get must be run as one of its subcommands")
			} else {
				fmt.Fprintf(stderr, "get must be run as one of its subcommands: unknown subcommand %q\n", args[0])
			}
			fmt.Fprintln(stderr, "Run 'cue help get' for known subcommands.")
			os.Exit(1) // TODO: get rid of this
			return nil
		}),
	}
	cmd.AddCommand(newGoCmd(c))
	return cmd
}
