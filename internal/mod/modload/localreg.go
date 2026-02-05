// Copyright 2025 CUE Authors
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

package modload

import (
	"context"

	"cuelang.org/go/internal/mod/modfiledata"
	"cuelang.org/go/mod/module"
)

// localReplacementRegistry wraps a Registry to handle local path replacements.
// When Requirements or Fetch is called for a module that has a local path
// replacement, it uses LocalReplacements to resolve the local path instead of
// delegating to the underlying registry.
type localReplacementRegistry struct {
	underlying   Registry
	localReplace *LocalReplacements
	replacements map[string]modfiledata.Replacement
}

// NewLocalReplacementRegistry creates a new registry that wraps the given registry
// and handles module replacements (both local path and remote module replacements).
// If there are no replacements, it returns the original registry unchanged.
//
// Returns an error if local replacements exist but the main module location cannot
// be resolved to an absolute path.
func NewLocalReplacementRegistry(reg Registry, mainModuleLoc module.SourceLoc, replacements map[string]modfiledata.Replacement) (Registry, error) {
	if len(replacements) == 0 {
		return reg, nil
	}
	// Create local replacements helper (may be nil if no local paths)
	lr, err := NewLocalReplacements(mainModuleLoc, replacements)
	if err != nil {
		return nil, err
	}
	return &localReplacementRegistry{
		underlying:   reg,
		localReplace: lr,
		replacements: replacements,
	}, nil
}

// Requirements implements modrequirements.Registry.
// For modules with local path replacements, it reads requirements from the
// local module's cue.mod/module.cue file. For all other modules (including
// remote replacements), it delegates to the underlying registry.
func (r *localReplacementRegistry) Requirements(ctx context.Context, m module.Version) ([]module.Version, error) {
	// Check if this module has a local path replacement
	if localPath := r.localReplace.LocalPathFor(m.Path()); localPath != "" {
		return r.localReplace.FetchRequirements(localPath)
	}
	// For non-local modules, delegate to underlying registry.
	// Note: remote replacements are handled by cueModSummary in requirements.go
	return r.underlying.Requirements(ctx, m)
}

// Fetch implements modpkgload.Registry.
// For modules with local path replacements, it returns a SourceLoc pointing
// to the local directory. For remote replacements, it fetches the replacement
// module. For non-replaced modules, it delegates to the underlying registry.
func (r *localReplacementRegistry) Fetch(ctx context.Context, m module.Version) (module.SourceLoc, error) {
	// Check if this module has a local path replacement
	if localPath := r.localReplace.LocalPathFor(m.Path()); localPath != "" {
		return r.localReplace.FetchSourceLoc(localPath)
	}
	// Check if this is a remote replacement - fetch the replacement module
	if repl, ok := r.replacements[m.Path()]; ok && repl.New.IsValid() {
		return r.underlying.Fetch(ctx, repl.New)
	}
	return r.underlying.Fetch(ctx, m)
}

// ModuleVersions implements Registry.
// This always delegates to the underlying registry since local modules don't
// have versions listed in a registry.
func (r *localReplacementRegistry) ModuleVersions(ctx context.Context, mpath string) ([]string, error) {
	return r.underlying.ModuleVersions(ctx, mpath)
}
