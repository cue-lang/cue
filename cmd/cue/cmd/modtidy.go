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
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"cuelang.org/go/internal/mod/modload"
)

func newModTidyCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tidy",
		Short: "download and tidy module dependencies",
		Long: `WARNING: THIS COMMAND IS EXPERIMENTAL.

Tidy resolves all module dependencies in the current module and updates
the cue.mod/module.cue file to reflect them.

It also removes dependencies that are not needed.

It will attempt to fetch modules that aren't yet present in the
dependencies by fetching the latest available version from
a registry.

See "cue help environment" for details on how $CUE_REGISTRY is used to
determine the modules registry.

Note: you must enable the modules experiment with:
	export CUE_EXPERIMENT=modules
for this command to work.
`,
		RunE: mkRunE(c, runModTidy),
		Args: cobra.ExactArgs(0),
	}

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
	ctx := context.Background()
	modRoot, err := findModuleRoot()
	if err != nil {
		return err
	}
	mf, err := modload.Tidy(ctx, os.DirFS(modRoot), ".", reg, versionForModFile())
	if err != nil {
		return err
	}
	// TODO check whether it's changed or not.
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

func findModuleRoot() (string, error) {
	// TODO this logic is duplicated in multiple places. We should
	// consider deduplicating it.
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "cue.mod")); err == nil {
			return dir, nil
		} else if !os.IsNotExist(err) {
			return "", err
		}
		dir1 := filepath.Dir(dir)
		if dir1 == dir {
			return "", fmt.Errorf("module root not found")
		}
		dir = dir1
	}
}
