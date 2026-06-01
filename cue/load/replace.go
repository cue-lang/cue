// Copyright 2026 CUE Authors
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

package load

import (
	"context"
	"fmt"
	"io/fs"
	"path"

	"cuelang.org/go/internal/mod/modpkgload"
	"cuelang.org/go/mod/modconfig"
	"cuelang.org/go/mod/modfile"
	"cuelang.org/go/mod/module"
)

// replacingRegistry wraps a modconfig.Registry so that replaced modules
// are served from their replacement sources.
type replacingRegistry struct {
	underlying modconfig.Registry
	repls      *modpkgload.Replacements
	openDir    func(path string) (fs.FS, error)
}

func newReplacingRegistry(reg modconfig.Registry, repls *modpkgload.Replacements, openDir func(string) (fs.FS, error)) modconfig.Registry {
	if repls == nil {
		return reg
	}
	if openDir == nil {
		openDir = func(p string) (fs.FS, error) {
			return module.OSDirFS(p), nil
		}
	}
	return &replacingRegistry{
		underlying: reg,
		repls:      repls,
		openDir:    openDir,
	}
}

func (r *replacingRegistry) replacement(m module.Version) (modpkgload.Replacement, bool) {
	return r.repls.Lookup(m.BasePath())
}

func (r *replacingRegistry) Fetch(ctx context.Context, m module.Version) (module.SourceLoc, error) {
	repl, ok := r.replacement(m)
	if !ok {
		return r.underlying.Fetch(ctx, m)
	}
	if repl.Dir != "" {
		fsys, err := r.openDir(repl.Dir)
		if err != nil {
			return module.SourceLoc{}, fmt.Errorf("cannot open replacement directory for %v: %v", m, err)
		}
		return module.SourceLoc{FS: fsys, Dir: "."}, nil
	}
	return r.underlying.Fetch(ctx, repl.Module)
}

func (r *replacingRegistry) Requirements(ctx context.Context, m module.Version) ([]module.Version, error) {
	repl, ok := r.replacement(m)
	if !ok {
		return r.underlying.Requirements(ctx, m)
	}
	if repl.Dir != "" {
		return r.requirementsFromDir(repl.Dir)
	}
	return r.underlying.Requirements(ctx, repl.Module)
}

func (r *replacingRegistry) DefaultMajorVersions(ctx context.Context, m module.Version) (map[string]string, error) {
	repl, ok := r.replacement(m)
	if !ok {
		return r.underlying.DefaultMajorVersions(ctx, m)
	}
	if repl.Dir != "" {
		return r.defaultMajorVersionsFromDir(repl.Dir)
	}
	return r.underlying.DefaultMajorVersions(ctx, repl.Module)
}

func (r *replacingRegistry) ModuleVersions(ctx context.Context, mpath string) ([]string, error) {
	return r.underlying.ModuleVersions(ctx, mpath)
}

func (r *replacingRegistry) requirementsFromDir(dir string) ([]module.Version, error) {
	mf, err := r.parseModFileFromDir(dir)
	if err != nil {
		return nil, err
	}
	return mf.DepVersions(), nil
}

func (r *replacingRegistry) defaultMajorVersionsFromDir(dir string) (map[string]string, error) {
	mf, err := r.parseModFileFromDir(dir)
	if err != nil {
		return nil, err
	}
	return mf.DefaultMajorVersions(), nil
}

func (r *replacingRegistry) parseModFileFromDir(dir string) (*modfile.File, error) {
	fsys, err := r.openDir(dir)
	if err != nil {
		return nil, fmt.Errorf("cannot open replacement directory: %v", err)
	}
	data, err := fs.ReadFile(fsys, path.Join(".", "cue.mod/module.cue"))
	if err != nil {
		return nil, fmt.Errorf("cannot read module file in replacement directory: %v", err)
	}
	mf, err := modfile.ParseNonStrict(data, path.Join(dir, "cue.mod/module.cue"))
	if err != nil {
		return nil, fmt.Errorf("cannot parse module file in replacement directory: %v", err)
	}
	return mf, nil
}
