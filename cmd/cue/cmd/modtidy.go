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
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"cuelang.org/go/internal/mod/modload"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/module"
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
`,
		RunE: mkRunE(c, runModTidy),
		Args: cobra.ExactArgs(0),
	}
	cmd.Flags().Bool(string(flagCheck), false, "check for tidiness after fetching dependencies; fail if module.cue would be updated")
	cmd.Flags().Bool(string(flagLocalOnly), false, "only update cue.mod/local-module.cue, leaving cue.mod/module.cue unchanged")

	return cmd
}

func runModTidy(cmd *Command, args []string) error {
	reg := getLazyRegistry()
	ctx := cmd.Context()
	modRoot, err := findModuleRoot()
	if err != nil {
		return err
	}
	opts := &modload.TidyOptions{
		LocForPath: dirReplaceLoc(modRoot),
		LocalOnly:  flagLocalOnly.Bool(cmd),
	}
	if flagCheck.Bool(cmd) {
		err := modload.CheckTidy(ctx, os.DirFS(modRoot), ".", reg, opts)
		return suggestModCommand(err)
	}
	res, err := modload.Tidy(ctx, os.DirFS(modRoot), ".", reg, opts)
	if err != nil {
		return suggestModCommand(err)
	}
	if res.Module != nil {
		if err := writeModuleFile(filepath.Join(modRoot, "cue.mod", "module.cue"), modfile.Format, res.Module); err != nil {
			return err
		}
	}
	localPath := filepath.Join(modRoot, "cue.mod", "local-module.cue")
	if res.Local != nil {
		// local-module.cue inherits its identity from module.cue and omits
		// versions that are redundant with it, so the published view is
		// needed as the base. With --local-only, module.cue is left
		// unchanged on disk, so read it from there.
		base := res.Module
		if base == nil {
			data, err := os.ReadFile(filepath.Join(modRoot, "cue.mod", "module.cue"))
			if err != nil {
				return err
			}
			base, err = modfile.ParseNonStrict(data, "cue.mod/module.cue")
			if err != nil {
				return err
			}
		}
		return writeModuleFile(localPath, func(mf *modfile.File) ([]byte, error) {
			return modfile.FormatLocal(mf, base)
		}, res.Local)
	}
	// No replaceWith fields remain (or --local-only with none): the
	// local-module.cue file serves no purpose, so remove it if present.
	if err := os.Remove(localPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// dirReplaceLoc returns a [modload.TidyOptions.LocForPath] function that
// resolves a directory replacement path relative to modRoot.
func dirReplaceLoc(modRoot string) func(string) (module.SourceLoc, error) {
	return func(p string) (module.SourceLoc, error) {
		if !filepath.IsAbs(p) {
			p = filepath.Join(modRoot, p)
		}
		return module.SourceLoc{
			FS:  module.OSDirFS(p),
			Dir: ".",
		}, nil
	}
}

// writeModuleFile formats mf with the given formatter and writes it to
// filename, skipping the write when the content is unchanged.
func writeModuleFile(filename string, format func(*modfile.File) ([]byte, error), mf *modfile.File) error {
	data, err := format(mf)
	if err != nil {
		return fmt.Errorf("internal error: invalid module file generated for %s: %v", filename, err)
	}
	oldData, err := os.ReadFile(filename)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return writeFileIfChanged(filename, oldData, data, 0o666)
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
