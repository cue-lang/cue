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

	"github.com/spf13/cobra"
)

// TODO: generate long description from documentation.

// cmdCmd represents the cmd command
var cmdCmd = &cobra.Command{
	Use:   "cmd <name> [-x] [instances]",
	Short: "run a user-defined shell command",
	Long: `cmd executes defined the named command for each of the named instances.

Commands define actions on instances. For example, they may specify
how to upload a configuration to Kubernetes. Commands are defined
directly in tool files, which are regular CUE files within the same
package with a filename ending in _tool.cue. These are typically
defined at the top of the module root so that they apply to all
instances.

Each command consists of one or more tasks. A task may load or write
a file, consult a user on the command line, fetch a web page, and
so on. Each task has inputs and outputs. Outputs are typically are
filled out by the task implementation as the task completes.

Inputs of tasks my refer to outputs of other tasks. The cue tool does
a static analysis of the configuration and only starts tasks that are
fully specified. Upon completion of each task, cue rewrites the instance,
filling in the completed task, and reevaluates which other tasks can
now start, and so on until all tasks have completed.

Commands are defined at the top-level of the configuration:

	command <Name>: { // from tool.Command
		// usage gives a short usage pattern of the command.
		// Example:
		//    fmt [-n] [-x] [packages]
		usage: Name | string

		// short gives a brief on-line description of the command.
		// Example:
		//    reformat package sources
		short: "" | string

		// long gives a detailed description of the command, including a
		// description of flags usage and examples.
		long: "" | string

		// A task defines a single action to be run as part of this command.
		// Each task can have inputs and outputs, depending on the type
		// task. The outputs are initially unspecified, but are filled out
		// by the tooling
		task <Name>: { // from "tool".Task
			// supported fields depend on type
		}

		VarValue = string | bool | int | float | [...string|int|float]

		// var declares values that can be set by command line flags or
		// environment variables.
		//
		// Example:
		//   // environment to run in
		//   var env: "test" | "prod"
		// The tool would print documentation of this flag as:
		//   Flags:
		//      --env string    environment to run in: test(default) or prod
		var <Name>: VarValue

		// flag defines a command line flag.
		//
		// Example:
		//   var env: "test" | "prod"
		//
		//   // augment the flag information for var
		//   flag env: {
		//       shortFlag:   "e"
		//       description: "environment to run in"
		//   }
		//
		// The tool would print documentation of this flag as:
		//   Flags:
		//     -e, --env string    environment to run in: test(default), staging, or prod
		//
		flag <Name>: { // from "tool".Flag
			// value defines the possible values for this flag.
			// The default is string. Users can define default values by
			// using disjunctions.
			value: env[Name].value | VarValue

			// name, if set, allows var to be set with the command-line flag
			// of the given name. null disables the command line flag.
			name: Name | null | string

			// short defines an abbreviated version of the flag.
			// Disabled by default.
			short: null | string
		}

		// populate flag with the default values for
		flag: { "\(k)": { value: v } | null for k, v in var }

		// env defines environment variables. It is populated with values
		// for var.
		//
		// To specify a var without an equivalent environment variable,
		// either specify it as a flag directly or disable the equally
		// named env entry explicitly:
		//
		//     var foo: string
		//     env foo: null  // don't use environment variables for foo
		//
		env <Name>: {
			// name defines the environment variable that sets this flag.
			name: "CUE_VAR_" + strings.Upper(Name) | string | null

			// The value retrieved from the environment variable or null
			// if not set.
			value: null | string | bytes
		}
		env: { "\(k)": { value: v } | null for k, v in var }
	}

Available tasks can be found in the package documentation at

	cuelang.org/pkg/tool.

More on tasks can be found in the tasks topic.

Examples:

A simple file using command line execution:

	$ cat <<EOF > hello_tool.cue
	package foo

	import "tool/exec"

	city: "Amsterdam"

	// Say hello!
	command hello: {
		// whom to say hello to
		var who: "World" | string

		task print: exec.Run({
			cmd: "echo Hello \(var.who)! Welcome to \(city)."
		})
	}
	EOF

	$ cue cmd echo
	Hello World! Welcome to Amsterdam.

	$ cue cmd echo -who you
	Hello you! Welcome to Amsterdam.


An example using pipes:

	package foo

	import "tool/exec"

	city: "Amsterdam"

	// Say hello!
	command hello: {
		var file: "out.txt" | string // save transcript to this file

		task ask: cli.Ask({
			prompt:   "What is your name?"
			response: string
		})

		// starts after ask
		task echo: exec.Run({
			cmd:    ["echo", "Hello", task.ask.response + "!"]
			stdout: string // capture stdout
		})

		// starts after echo
		task write: file.Append({
			filename: var.file
			contents: task.echo.stdout
		})

		// also starts after echo
		task print: cli.Print({
			contents: task.echo.stdout
		})
	}

`,
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("cmd run but shouldn't")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(cmdCmd)
}
