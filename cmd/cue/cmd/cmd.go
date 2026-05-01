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

	"cuelang.org/go/cue/errors"
	"github.com/spf13/cobra"
)

// TODO: generate long description from documentation.

func newCmdCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cmd <name> [inputs]",
		Short: "run a user-defined workflow command",
		Long: `cmd executes the named workflow command for each of the named instances.

Workflow commands are defined in tool files, which are regular CUE
files within the same package with a filename ending in _tool.cue.

Run "cue help commands" for more details on authoring tasks and
workflow commands.
`,
		RunE: mkRunE(c, func(cmd *Command, args []string) error {
			if len(args) == 0 {
				w := cmd.ErrOrStderr()
				fmt.Fprintln(w, "cmd must be run as one of its subcommands")
				fmt.Fprintln(w, "Run 'cue help cmd' for known subcommands.")
				return ErrPrintedError
			}
			tools, err := buildTools(cmd, args[1:])
			if err != nil {
				return err
			}
			sub, err := customCommand(cmd, commandSection, args[0], tools)
			if err != nil {
				w := cmd.ErrOrStderr()
				fmt.Fprint(w, errors.Details(err, &errors.Config{Cwd: rootWorkingDir()}))
				fmt.Fprintln(w, `Ensure custom commands are defined in a "_tool.cue" file.`)
				fmt.Fprintln(w, "Run 'cue help cmd' to list available custom commands.")
				return ErrPrintedError
			}
			// Presumably the *cobra.Command argument should be cmd.Command,
			// as that is the one which will have the right settings applied.
			return sub.RunE(cmd.Command, args[1:])
		}),
	}

	addInjectionFlags(cmd)

	// Load custom commands from the current package so that `cue cmd --help`
	// lists them. The guard ensures we only do this for `cue cmd` itself,
	// since subcommands inherit our HelpFunc via cobra's parent walk.
	cmdCmd := cmd
	defaultHelp := cmd.HelpFunc()
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if cmd == cmdCmd && !cmd.HasSubCommands() {
			if tools, err := buildTools(c, nil); err == nil {
				addCustomCommands(c, cmd, commandSection, tools)
			}
		}
		defaultHelp(cmd, args)
	})

	return cmd
}
