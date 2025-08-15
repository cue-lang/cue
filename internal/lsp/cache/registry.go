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

package cache

import (
	"context"
	"errors"

	"cuelang.org/go/internal/lsp/fscache"
	"cuelang.org/go/mod/module"
)

// RegistryWrapper is a wrapper around any existing [Registry]
// implementation.
type RegistryWrapper struct {
	Registry
	overlayFS *fscache.OverlayFS
}

// Fetch implements (and wraps) [modpkgload.Registry]. It modifies
// locations returned by Registry.Fetch, switching the FS to the
// wrapper's internal [fscache.OverlayFS].
func (reg *RegistryWrapper) Fetch(ctx context.Context, m module.Version) (module.SourceLoc, error) {
	loc, err := reg.Registry.Fetch(ctx, m)
	if err != nil {
		return module.SourceLoc{}, err
	}
	modFS, ok := loc.FS.(module.OSRootFS)
	if !ok {
		return module.SourceLoc{}, errors.New("cannot wrap non-OSRootFS")
	}

	loc.FS = reg.overlayFS.IoFS(modFS.OSRoot())
	return loc, nil
}
