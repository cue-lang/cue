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
