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
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"cuelang.org/go/internal/httplog"
	"cuelang.org/go/internal/mod/modload"
	"cuelang.org/go/mod/modfile"
)

func newModGetCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get",
		Short: "add and upgrade module dependencies",
		Long: `Get updates module dependencies, fetching new dependencies if
needed and changing versions to specified versions. It can downgrade
a version only when a higher version is not required by other
dependencies.

Each argument specifies a module path and optionally a version
suffix. If there is no version suffix, the latest non-prerelease version
of the module will be requested; alternatively a suffix of "@latest"
also specifies the latest version.

A version suffix can contain a major version only (@v1), a major and minor
version (@v1.2) or full version (@v1.2.3). If minor or patch version is omitted, the
latest non-prerelease version will be chosen that has the same major
and minor versions.

If the desired version cannot be chosen (for example because a
dependency already uses a later version than the desired version),
this command will fail.

See "cue help environment" for details on how $CUE_REGISTRY is used to
determine the modules registry.

Note that this command is not yet stable and may be changed.
`,
		RunE: mkRunE(c, runModGet),
		Args: cobra.MinimumNArgs(1),
	}

	return cmd
}

func runModGet(cmd *Command, args []string) error {
	reg, err := getCachedRegistry()
	if err != nil {
		return err
	}
	ctx := backgroundContext()
	modRoot, err := findModuleRoot()
	if err != nil {
		return err
	}
	mf, err := modload.UpdateVersions(ctx, os.DirFS(modRoot), ".", reg, args)
	if err != nil {
		return suggestModCommand(err)
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

func readModuleFile() (string, *modfile.File, []byte, error) {
	modRoot, err := findModuleRoot()
	if err != nil {
		return "", nil, nil, err
	}
	modPath := filepath.Join(modRoot, "cue.mod", "module.cue")
	data, err := os.ReadFile(modPath)
	if err != nil {
		return "", nil, nil, err
	}
	mf, err := modfile.ParseNonStrict(data, modPath)
	if err != nil {
		return "", nil, nil, err
	}
	return modPath, mf, data, nil
}

func findModuleRoot() (string, error) {
	// TODO this logic is duplicated in multiple places. We should
	// consider deduplicating it.
	dir := rootWorkingDir
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

func backgroundContext() context.Context {
	// TODO move this into the ociregistry module
	return httplog.ContextWithAllowedURLQueryParams(
		context.Background(),
		allowURLQueryParam,
	)
}

// The set of query string keys that we expect to send as part of the OCI
// protocol. Anything else is potentially dangerous to leak, as it's probably
// from a redirect. These redirects often included tokens or signed URLs.
// TODO move this into the ociregistry module.
var paramAllowlist = map[string]bool{
	// Token exchange
	"scope":   true,
	"service": true,
	// Cross-repo mounting
	"mount": true,
	"from":  true,
	// Layer PUTerror
	"digest": true,
	// Listing tags and catalog
	"n":    true,
	"last": true,
}

func allowURLQueryParam(k string) bool {
	return paramAllowlist[k]
}
