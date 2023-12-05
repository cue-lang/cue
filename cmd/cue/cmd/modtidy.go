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
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/internal/mod/modfile"
	"cuelang.org/go/internal/mod/modload"
	"cuelang.org/go/internal/mod/modpkgload"
	"cuelang.org/go/internal/mod/modregistry"
	"cuelang.org/go/internal/mod/modrequirements"
	"cuelang.org/go/internal/mod/module"
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
	reg, err := getRegistry()
	if err != nil {
		return err
	}
	if reg == nil {
		return fmt.Errorf("no registry configured to upload to")
	}
	ctx := context.Background()
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	loadReg := &modloadRegistry{modregistry.NewClient(reg)}
	mf, err := modload.Load(ctx, os.DirFS("/"), strings.TrimPrefix(wd, "/"), loadReg)
	if err != nil {
		return err
	}
	// TODO check whether it's changed or not.
	cuectx := cuecontext.New()
	v := cuectx.Encode(mf)
	if err := os.WriteFile("cue.mod/module.cue", []byte(fmt.Sprint(v)+"\n"), 0o666); err != nil {
		return err
	}
	return nil
}

type modloadRegistry struct {
	reg *modregistry.Client
}

func (r *modloadRegistry) CUEModSummary(ctx context.Context, mv module.Version) (*modrequirements.ModFileSummary, error) {
	m, err := r.reg.GetModule(ctx, mv)
	if err != nil {
		return nil, err
	}
	data, err := m.ModuleFile(ctx)
	if err != nil {
		return nil, fmt.Errorf("cannot get module file from %v: %v", m, err)
	}
	mf, err := modfile.Parse(data, mv.String())
	if err != nil {
		return nil, fmt.Errorf("cannot parse module file from %v: %v", m, err)
	}
	return &modrequirements.ModFileSummary{
		Require: mf.DepVersions(),
		Module:  mv,
	}, nil
}

// getModContents downloads the module with the given version
// and returns the directory where it's stored.
func (c *modloadRegistry) Fetch(ctx context.Context, mv module.Version) (_loc modpkgload.SourceLoc, _ error) {
	m, err := c.reg.GetModule(ctx, mv)
	if err != nil {
		return modpkgload.SourceLoc{}, err
	}
	r, err := m.GetZip(ctx)
	if err != nil {
		return modpkgload.SourceLoc{}, err
	}
	defer r.Close()
	zipData, err := io.ReadAll(r)
	if err != nil {
		return modpkgload.SourceLoc{}, err
	}
	zipr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return modpkgload.SourceLoc{}, err
	}
	return modpkgload.SourceLoc{
		FS:  zipr,
		Dir: ".",
	}, nil
}

func (r *modloadRegistry) ModuleVersions(ctx context.Context, mpath string) ([]string, error) {
	return r.reg.ModuleVersions(ctx, mpath)
}
