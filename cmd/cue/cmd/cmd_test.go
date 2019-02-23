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
	"testing"

	"cuelang.org/go/cue/errors"
	"github.com/spf13/cobra"
)

func TestCmd(t *testing.T) {
	testCases := []string{
		"echo",
		"run",
		"run_list",
		"baddisplay",
		"http",
	}
	for _, name := range testCases {
		run := func(cmd *cobra.Command, args []string) error {
			testOut = cmd.OutOrStdout()
			defer func() { testOut = nil }()

			tools := buildTools(rootCmd, args)
			cmd, err := addCustom(rootCmd, "command", name, tools)
			if err != nil {
				return err
			}
			err = executeTasks("command", name, tools)
			if err != nil {
				errors.Print(testOut, err)
			}
			return nil
		}
		runCommand(t, run, "cmd_"+name)
	}
}
