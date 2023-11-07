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
	"context"
	"fmt"
	"os"

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

		Use:   "upload <version>",
		Short: "upload the current module to a registry",
		Long: `WARNING: THIS COMMAND IS EXPERIMENTAL ONLY.

Upload the current module to an OCI registry.
Currently this command must be run in the module's root directory.
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
		return fmt.Errorf("no registry configured to upload to")
	}
	modfileData, err := os.ReadFile("cue.mod/module.cue")
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no cue.mod/module.cue file found; cue mod upload must be run in the module's root directory")
		}
		return err
	}
	mf, err := modfile.Parse(modfileData, "cue.mod/module.cue")
	if err != nil {
		return err
	}
	mv, err := module.NewVersion(mf.Module, args[0])
	if err != nil {
		return fmt.Errorf("cannot form module version: %v", err)
	}
	zf, err := os.CreateTemp("", "cue-upload-")
	if err != nil {
		return err
	}
	defer os.Remove(zf.Name())
	defer zf.Close()

	// TODO verify that all dependencies exist in the registry.
	if err := modzip.CreateFromDir(zf, mv, "."); err != nil {
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
	fmt.Printf("uploaded %s\n", mv)
	return nil
}
