// Copyright 2025 CUE Authors
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
	"github.com/spf13/cobra"
)

func newRefactorCmd(c *Command) *cobra.Command {
	cmd := commandGroup(&cobra.Command{
		// Experimental so far.
		Hidden: true,

		Use:   "refactor <cmd> [arguments]",
		Short: "refactor and rewrite CUE code",
		Long: `
This command groups together commands relating
to altering code within the current CUE module.
`[1:],
	})

	cmd.AddCommand(newRefactorImportsCmd(c))
	return cmd
}
