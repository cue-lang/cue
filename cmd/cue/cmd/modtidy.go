// Copyright 2023 The CUE Authors
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
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"cuelang.org/go/internal/cueversion"
	"cuelang.org/go/internal/mod/modload"
)

func newModTidyCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tidy",
		Short: "download and tidy module dependencies",
		Long: `Tidy resolves all module dependencies in the current module and updates
the cue.mod/module.cue file to reflect them.

It also removes dependencies that are not needed.

It will attempt to fetch modules that aren't yet present in the
dependencies by fetching the latest available version from
a registry.

See "cue help environment" for details on how $CUE_REGISTRY is used to
determine the modules registry.

Note that this command is not yet stable and may be changed.
`,
		RunE: mkRunE(c, runModTidy),
		Args: cobra.ExactArgs(0),
	}
	cmd.Flags().Bool(string(flagCheck), false, "check for tidiness only; do not update module.cue file")

	return cmd
}

func runModTidy(cmd *Command, args []string) error {
	reg, err := getCachedRegistry()
	if err != nil {
		return err
	}
	if reg == nil {
		return fmt.Errorf("modules experiment not enabled (enable with CUE_EXPERIMENT=modules)")
	}
	ctx := backgroundContext()
	modRoot, err := findModuleRoot()
	if err != nil {
		return err
	}
	if flagCheck.Bool(cmd) {
		// TODO: Once CheckTidy returns a structured error with the reason why a module isn't tidy,
		// we can make better errors like:
		//
		//    tidy the module via 'cue mod tidy': no recorded dependency provides package ...
		err := modload.CheckTidy(ctx, os.DirFS(modRoot), ".", reg)
		notTidyErr := new(modload.ErrModuleNotTidy)
		if errors.As(err, &notTidyErr) {
			if notTidyErr.Reason == "" {
				err = fmt.Errorf("module is not tidy, use 'cue mod tidy'")
			} else {
				err = fmt.Errorf("module is not tidy, use 'cue mod tidy': %v", notTidyErr.Reason)
			}
		}
		return err
	}
	mf, err := modload.Tidy(ctx, os.DirFS(modRoot), ".", reg, cueversion.LanguageVersion())
	if err != nil {
		return err
	}
	data, err := mf.Format()
	if err != nil {
		return fmt.Errorf("internal error: invalid module.cue file generated: %v", err)
	}
	modPath := filepath.Join(modRoot, "cue.mod", "module.cue")
	oldData, err := os.ReadFile(modPath)
	if err != nil {
		// Shouldn't happen because modload.Load returns an error
		// if it can't load the module file.
		return err
	}
	if bytes.Equal(data, oldData) {
		return nil
	}
	if err := os.WriteFile(modPath, data, 0o666); err != nil {
		return err
	}
	return nil
}
