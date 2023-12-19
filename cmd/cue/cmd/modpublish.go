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

	"cuelang.org/go/internal/mod/modfile"
	"cuelang.org/go/internal/mod/modregistry"
	"cuelang.org/go/internal/mod/module"
	"cuelang.org/go/internal/mod/modzip"
)

func newModUploadCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		// TODO: this command is still experimental, don't show it in
		// the documentation just yet.
		Hidden: true,

		Use:   "publish <version>",
		Short: "publish the current module to a registry",
		Long: `WARNING: THIS COMMAND IS EXPERIMENTAL.

Publish the current module to an OCI registry.
Also note that this command does no dependency or other checks at the moment.
`,
		RunE: mkRunE(c, runModUpload),
		Args: cobra.ExactArgs(1),
	}

	return cmd
}

func runModUpload(cmd *Command, args []string) error {
	reg, err := getRegistry()
	if err != nil {
		return err
	}
	if reg == nil {
		return fmt.Errorf("no registry configured to publish to")
	}
	modRoot, err := findModuleRoot()
	if err != nil {
		return err
	}
	// TODO ensure module tidiness.
	modPath := filepath.Join(modRoot, "cue.mod/module.cue")
	modfileData, err := os.ReadFile(modPath)
	if err != nil {
		return err
	}
	mf, err := modfile.Parse(modfileData, modPath)
	if err != nil {
		return err
	}
	mv, err := module.NewVersion(mf.Module, args[0])
	if err != nil {
		return fmt.Errorf("cannot form module version: %v", err)
	}
	zf, err := os.CreateTemp("", "cue-publish-")
	if err != nil {
		return err
	}
	defer os.Remove(zf.Name())
	defer zf.Close()

	// TODO verify that all dependencies exist in the registry.
	if err := modzip.CreateFromDir(zf, mv, modRoot); err != nil {
		return err
	}
	info, err := zf.Stat()
	if err != nil {
		return err
	}

	rclient := modregistry.NewClient(reg)
	if err := rclient.PutModule(context.Background(), mv, zf, info.Size()); err != nil {
		return fmt.Errorf("cannot put module: %v", err)
	}
	fmt.Printf("published %s\n", mv)
	return nil
}
