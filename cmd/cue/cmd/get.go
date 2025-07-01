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
	"github.com/spf13/cobra"
)

func newGetCmd(c *Command) *cobra.Command {
	cmd := commandGroup(&cobra.Command{
		Use:   "get <language> [packages]",
		Short: "add non-CUE dependencies to the current module",
		Long: `Get downloads packages or modules for non-CUE languages
to include them in the module's pkg directory.

Get requires an additional language field to determine for which
language definitions should be fetched. Definitions are extracted
from the source of the respective language and stored.
The specifics on how dependencies are fetched and converted vary
per language and are documented in the respective subcommands.

For information on native CUE modules:

    cue help modules
`,
	})
	cmd.AddCommand(newGoCmd(c))
	cmd.AddCommand(newCRDCmd(c))
	return cmd
}
