// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import (
	"cuelang.org/go/internal/lsp/fscache"
	"cuelang.org/go/internal/mod/modpkgload"
	"cuelang.org/go/internal/mod/modrequirements"
	"cuelang.org/go/mod/modconfig"
)

// NewCache creates a new Cache. If registry is nil, a new registry is
// made using [modconfig.NewRegistry].
func NewCache(registry modregistry) (*Cache, error) {
	if registry == nil {
		modcfg := &modconfig.Config{
			ClientType: "cuelsp",
		}
		var err error
		registry, err = modconfig.NewRegistry(modcfg)
		if err != nil {
			return nil, err
		}
	}

	return &Cache{
		fs:       fscache.NewCUECachedFS(),
		registry: registry,
	}, nil
}

// A Cache holds content that is shared across multiple cuelsp
// client/editor connections.
type Cache struct {
	fs       *fscache.CUECacheFS
	registry modregistry
}

type modregistry interface {
	modrequirements.Registry
	modpkgload.Registry
}
