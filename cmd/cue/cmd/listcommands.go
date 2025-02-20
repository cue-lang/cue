// Copyright 2019 The CUE Authors
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

func newListCommandsCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Hidden: true,
		Use:    "listcommands",
		Short:  "List all cue commands",
		RunE:   mkRunE(c, runListCommands),
	}

	return cmd
}

func runListCommands(cmd *Command, args []string) (err error) {
	listCommands(cmd.root, "cue")
	return nil
}

func listCommands(cmd *cobra.Command, parent string) {
	for _, c := range cmd.Commands() {
		fullCmd := parent + " " + c.Name()
		fmt.Println(fullCmd)
		listCommands(c, fullCmd)
	}
}
