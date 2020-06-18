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

var validCompletionArgs = []string{"bash", "zsh", "fish", "powershell"}

const completionExample = `
Bash:

$ source <(cue completion bash)

# To load completions for each session, execute once:
Linux:
  $ cue completion bash > /etc/bash_completion.d/cue
MacOS:
  $ cue completion bash > /usr/local/etc/bash_completion.d/cue

Zsh:

$ source <(cue completion zsh)

# To load completions for each session, execute once:
$ cue completion zsh > "${fpath[1]}/_cue"

Fish:

$ cue completion fish | source

# To load completions for each session, execute once:
$ cue completion fish > ~/.config/fish/completions/cue.fish
`

func newCompletionCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:       fmt.Sprintf("completion %s", validCompletionArgs),
		Short:     "Generate completion script",
		Long:      ``,
		Example:   completionExample,
		ValidArgs: validCompletionArgs,
		Args:      cobra.ExactValidArgs(1),
		RunE:      mkRunE(c, runCompletion),
	}
	return cmd
}

func runCompletion(cmd *Command, args []string) error {
	w := cmd.OutOrStdout()
	switch args[0] {
	case "bash":
		cmd.Root().GenBashCompletion(w)
	case "zsh":
		cmd.Root().GenZshCompletion(w)
	case "fish":
		cmd.Root().GenFishCompletion(w, true)
	case "powershell":
		cmd.Root().GenPowerShellCompletion(w)
	default:
		return fmt.Errorf("%s is not a supported shell", args[0])
	}
	return nil
}
