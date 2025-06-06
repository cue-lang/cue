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
		Long: `cmd executes the named command for each of the named instances.

Workflow commands define actions on instances. For example, they
may specify how to upload a configuration to Kubernetes. Workflow
commands are defined directly in tool files, which are regular
CUE files within the same package with a filename ending in
_tool.cue.  These are typically defined at the module root so
that they apply to all instances.

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

	https://cuelang.org/go/pkg/tool#section-directories

Examples:

In this simple example, we define a workflow command called
"hello", which declares a single task called "print" which uses
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

We run the "hello" workflow command like this:

	$ cue cmd hello
	Hello World! Welcome to Amsterdam.

	$ cue cmd --inject who=Jan hello
	Hello Jan! Welcome to Amsterdam.


In this example we declare the "prompted" workflow command which
has four tasks. The first task prompts the user for a string
input. The second task depends on the first, and echos the
response back to the user with a friendly message. The third task
pipes the output from the second to a file. The fourth task pipes
the output from the second to standard output (i.e. it echos it
again).

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

Run "cue help commands" for more details on tasks and workflow commands.
`,
		RunE: mkRunE(c, func(cmd *Command, args []string) error {
			if len(args) == 0 {
				w := cmd.OutOrStderr()
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
				w := cmd.OutOrStderr()
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

	addInjectionFlags(cmd.Flags(), true, false)

	return cmd
}
