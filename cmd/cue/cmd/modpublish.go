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
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"cuelang.org/go/internal/vcs"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/modregistry"
	"cuelang.org/go/mod/module"
	"cuelang.org/go/mod/modzip"
)

func newModUploadCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "publish <version>",
		Short: "publish the current module to a registry",
		Long: `WARNING: THIS COMMAND IS EXPERIMENTAL.

Publish the current module to an OCI registry. It consults
$CUE_REGISTRY to determine where the module should be published (see
"cue help environment" for details). Also note that this command does
no dependency or other checks at the moment.

Note: you must enable the modules experiment with:
	export CUE_EXPERIMENT=modules
for this command to work.
`,
		RunE: mkRunE(c, runModUpload),
		Args: cobra.ExactArgs(1),
	}

	return cmd
}

func runModUpload(cmd *Command, args []string) error {
	ctx := cmd.Context()
	resolver, err := getRegistryResolver()
	if err != nil {
		return err
	}
	if resolver == nil {
		return fmt.Errorf("modules experiment not enabled (enable with CUE_EXPERIMENT=modules)")
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
	if mf.Source == nil {
		// TODO print filename relative to current directory
		return fmt.Errorf("no source field found in cue.mod/module.cue")
	}
	zf, err := os.CreateTemp("", "cue-publish-")
	if err != nil {
		return err
	}
	defer os.Remove(zf.Name())
	defer zf.Close()

	// TODO verify that all dependencies exist in the registry.

	var vcsStatus vcs.Status

	switch mf.Source.Kind {
	case "git":
		vcsImpl, err := vcs.New(mf.Source.Kind, modRoot)
		if err != nil {
			return err
		}
		status, err := vcsImpl.Status(ctx)
		if err != nil {
			return err
		}
		if status.Uncommitted {
			// TODO implement --force to bypass this check
			return fmt.Errorf("VCS state is not clean")
		}
		files, err := vcsImpl.ListFiles(ctx, modRoot)
		if err != nil {
			return err
		}
		if err := modzip.Create[string](zf, mv, files, osFileIO{
			modRoot: modRoot,
		}); err != nil {
			return err
		}
		vcsStatus = status
	case "none":
		if err := modzip.CreateFromDir(zf, mv, modRoot); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unrecognized source kind %q", mf.Source.Kind)
	}
	info, err := zf.Stat()
	if err != nil {
		return err
	}

	rclient := modregistry.NewClientWithResolver(resolver)
	_ = vcsStatus // TODO attach vcsStatus to PutModule metadata
	if err := rclient.PutModule(backgroundContext(), mv, zf, info.Size()); err != nil {
		return fmt.Errorf("cannot put module: %v", err)
	}
	fmt.Printf("published %s\n", mv)
	return nil
}

// osFileIO implements [modzip.FileIO] for filepath
// paths relative to the module root directory, as returned
// by [vcs.VCS.ListFiles].
type osFileIO struct {
	modRoot string
}

func (osFileIO) Path(f string) string {
	return filepath.ToSlash(f)
}

func (fio osFileIO) Lstat(f string) (fs.FileInfo, error) {
	return os.Lstat(fio.absPath(f))
}

func (fio osFileIO) Open(f string) (io.ReadCloser, error) {
	return os.Open(fio.absPath(f))
}

func (fio osFileIO) absPath(f string) string {
	return filepath.Join(fio.modRoot, f)
}
