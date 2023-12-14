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
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"cuelang.org/go/internal/mod/modload"
)

func newModTidyCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		// TODO: this command is still experimental, don't show it in
		// the documentation just yet.
		Hidden: true,

		Use:   "tidy",
		Short: "download and tidy module dependencies",
		Long: `WARNING: THIS COMMAND IS EXPERIMENTAL.

Currently this command must be run in the module's root directory.
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
		return fmt.Errorf("no registry configured to upload to")
	}
	ctx := context.Background()
	// TODO don't assume we're running in the module's root directory.
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	mf, err := modload.Load(ctx, os.DirFS(wd), ".", reg)
	if err != nil {
		return err
	}
	// TODO check whether it's changed or not.
	data, err := mf.Format()
	if err != nil {
		return fmt.Errorf("internal error: invalid module.cue file generated: %v", err)
	}
	if err := os.WriteFile(filepath.Join("cue.mod", "module.cue"), data, 0o666); err != nil {
		return err
	}
	return nil
}

func modCacheDir() (string, error) {
	if dir := os.Getenv("CUE_MODCACHE"); dir != "" {
		return dir, nil
	}
	sysCacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine system cache directory: %v", err)
	}
	// TODO rethink cache namespace as per comments in https://review.gerrithub.io/c/cue-lang/cue/+/1173535/18
	return filepath.Join(sysCacheDir, "cue"), nil
}
