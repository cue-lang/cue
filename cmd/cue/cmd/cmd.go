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

// TODO: generate long description from documentation.

func newCmdCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cmd <name> [inputs]",
		Short: "run a user-defined shell command",
		Long: `cmd executes the named command for each of the named instances.

Commands define actions on instances. For example, they may
specify how to upload a configuration to Kubernetes. Commands are
defined directly in tool files, which are regular CUE files
within the same package with a filename ending in _tool.cue.
These are typically defined at the module root so that they apply
to all instances.

Each command consists of one or more tasks. A task may, for
example, load or write a file, consult a user on the command
line, fetch a web page, and so on. Each task has inputs and
outputs. Outputs are typically filled out by the task
implementation as the task completes.

Inputs of tasks my refer to outputs of other tasks. The cue tool
does a static analysis of the configuration and only starts tasks
that are fully specified. Upon completion of each task, cue
rewrites the instance, filling in the completed task, and
reevaluates which other tasks can now start, and so on until all
tasks have completed.

Available tasks can be found in the package documentation at

	https://pkg.go.dev/cuelang.org/go/pkg/tool?tab=subdirectories

Examples:

In this simple example, we define a command called "hello",
which declares a single task called "print" which uses
"tool/exec.Run" to execute a shell command that echos output to
the terminal:

	$ cat <<EOF > hello_tool.cue
	package foo

	import "tool/exec"

	city: "Amsterdam"
	who: *"World" | string @tag(who)

	// Say hello!
	command: hello: {
		print: exec.Run & {
			cmd: "echo Hello \(who)! Welcome to \(city)."
		}
	}
	EOF

We run the "hello" command like this:

	$ cue cmd hello
	Hello World! Welcome to Amsterdam.

	$ cue cmd --inject who=Jan hello
	Hello Jan! Welcome to Amsterdam.


In this example we declare the "prompted" command which has four
tasks. The first task prompts the user for a string input. The
second task depends on the first, and echos the response back to
the user with a friendly message. The third task pipes the output
from the second to a file. The fourth task pipes the output from
the second to standard output (i.e. it echos it again).

	package foo

	import (
		"tool/cli"
		"tool/exec"
		"tool/file"
	)

	city: "Amsterdam"

	// Say hello!
	command: prompter: {
		// save transcript to this file
		var: file: *"out.txt" | string @tag(file)

		ask: cli.Ask & {
			prompt:   "What is your name?"
			response: string
		}

		// starts after ask
		echo: exec.Run & {
			cmd:    ["echo", "Hello", ask.response + "!"]
			stdout: string // capture stdout
		}

		// starts after echo
		append: file.Append & {
			filename: var.file
			contents: echo.stdout
		}

		// also starts after echo
		print: cli.Print & {
			text: echo.stdout
		}
	}

Run "cue help commands" for more details on tasks and commands.
`,
		RunE: mkRunE(c, func(cmd *Command, args []string) error {
			w := cmd.Stderr()
			if len(args) == 0 {
				fmt.Fprintln(w, "cmd must be run as one of its subcommands")
			} else {
				const msg = `cmd must be run as one of its subcommands: unknown subcommand %q
Ensure commands are defined in a "_tool.cue" file.
`
				fmt.Fprintf(w, msg, args[0])
			}
			fmt.Fprintln(w, "Run 'cue help cmd' for known subcommands.")
			os.Exit(1) // TODO: get rid of this
			return nil
		}),
	}

	cmd.Flags().SetInterspersed(false)

	addInjectionFlags(cmd.Flags(), true)

	return cmd
}
