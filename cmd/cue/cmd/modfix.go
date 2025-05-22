// Copyright 2024 The CUE Authors
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
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"cuelang.org/go/mod/modfile"
	"github.com/spf13/cobra"
)

func newModFixCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fix",
		Short: "fix a legacy cue.mod/module.cue file",
		Long: `Fix provides a way to migrate from a legacy module.cue file
to the new standard syntax. It

- adds a language.version field
- moves unrecognized fields into the custom.legacy field
- adds a major version to the module path

If there is no module path, it chooses an arbitrary path (test.example@v0).

If the module.cue file is already compatible with the new syntax,
it is just formatted without making any other changes.

Note that this command is not yet stable and may be changed.
`,
		RunE: mkRunE(c, runModFix),
		Args: cobra.ExactArgs(0),
	}
	return cmd
}

func runModFix(cmd *Command, args []string) error {
	modRoot, err := findModuleRoot()
	if err != nil {
		return err
	}
	modPath := filepath.Join(modRoot, "cue.mod", "module.cue")
	data, err := os.ReadFile(modPath)
	if err != nil {
		return err
	}
	mf, err := modfile.FixLegacy(data, modPath)
	if err != nil {
		return err
	}
	newData, err := modfile.Format(mf)
	if err != nil {
		return fmt.Errorf("internal error: invalid module.cue file generated: %v", err)
	}
	if bytes.Equal(newData, data) {
		return nil
	}
	if err := os.WriteFile(modPath, newData, 0o666); err != nil {
		return err
	}
	return nil
}
