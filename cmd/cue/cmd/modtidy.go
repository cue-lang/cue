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

	"cuelang.org/go/internal/mod/modload"
	"cuelang.org/go/mod/modfile"
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
	cmd.Flags().Bool(string(flagCheck), false, "check for tidiness after fetching dependencies; fail if module.cue would be updated")

	return cmd
}

func runModTidy(cmd *Command, args []string) error {
	reg, err := getCachedRegistry()
	if err != nil {
		return err
	}
	ctx := backgroundContext()
	modRoot, err := findModuleRoot()
	if err != nil {
		return err
	}
	if flagCheck.Bool(cmd) {
		err := modload.CheckTidy(ctx, os.DirFS(modRoot), ".", reg)
		return suggestModCommand(err)
	}
	mf, err := modload.Tidy(ctx, os.DirFS(modRoot), ".", reg)
	if err != nil {
		return suggestModCommand(err)
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

// suggestModCommand rewrites a non-nil error to suggest to the user
// what command they could use to fix a problem.
// [modload.ErrModuleNotTidy] suggests running `cue mod tidy`,
// and [modfile.ErrNoLanguageVersion] suggests running `cue mod fix`.
func suggestModCommand(err error) error {
	notTidyErr := new(modload.ErrModuleNotTidy)
	switch {
	case errors.Is(err, modfile.ErrNoLanguageVersion):
		// TODO(mvdan): note that we cannot use standard Go error wrapping here via %w
		// as then errors.Print calls errors.Errors which reaches for the first CUE error
		// via errors.As, skipping over any non-CUE-wrapped errors.
		err = fmt.Errorf("%v; run 'cue mod fix'", err)
	case errors.As(err, &notTidyErr):
		if notTidyErr.Reason == "" {
			err = fmt.Errorf("module is not tidy, use 'cue mod tidy'")
		} else {
			err = fmt.Errorf("module is not tidy, use 'cue mod tidy': %v", notTidyErr.Reason)
		}
	}
	return err
}
