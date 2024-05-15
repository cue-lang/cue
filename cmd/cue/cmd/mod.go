// Copyright 2019 CUE Authors
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

	"github.com/spf13/cobra"
)

func newModCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mod <cmd> [arguments]",
		Short: "module maintenance",
		Long: `Mod groups commands which operate on CUE modules.

Note that support for modules is built into all the cue commands, not
just 'cue mod'.

See also:
	cue help modules
`,
		RunE: mkRunE(c, func(cmd *Command, args []string) error {
			stderr := cmd.Stderr()
			if len(args) == 0 {
				fmt.Fprintln(stderr, "mod must be run as one of its subcommands")
			} else {
				fmt.Fprintf(stderr, "mod must be run as one of its subcommands: unknown subcommand %q\n", args[0])
			}
			fmt.Fprintln(stderr, "Run 'cue help mod' for known subcommands.")
			return ErrPrintedError
		}),
	}

	cmd.AddCommand(newModEditCmd(c))
	cmd.AddCommand(newModFixCmd(c))
	cmd.AddCommand(newModGetCmd(c))
	cmd.AddCommand(newModInitCmd(c))
	cmd.AddCommand(newModRegistryCmd(c))
	cmd.AddCommand(newModResolveCmd(c))
	cmd.AddCommand(newModTidyCmd(c))
	cmd.AddCommand(newModUploadCmd(c))
	return cmd
}
