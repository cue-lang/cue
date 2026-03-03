// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import (
	"cuelang.org/go/internal/lsp/fscache"
	"cuelang.org/go/internal/mod/modpkgload"
	"cuelang.org/go/internal/mod/modrequirements"
	"cuelang.org/go/mod/modconfig"
	"cuelang.org/go/unstable/lspaux/config"
)

// New creates a new Cache.
func New(extProfile *config.Profile) (*Cache, error) {
	modcfg := &modconfig.Config{
		ClientType: "cuelsp",
	}
	reg, err := modconfig.NewRegistry(modcfg)
	if err != nil {
		return nil, err
	}
	return NewWithRegistry(extProfile, reg), nil
}

// NewWithRegistry creates a new cache, using the specified registry.
func NewWithRegistry(extProfile *config.Profile, reg Registry) *Cache {
	if reg == nil {
		panic("nil registry")
	}

	return &Cache{
		fs:         fscache.NewCUECachedFS(),
		registry:   reg,
		extProfile: extProfile,
	}
}

// A Cache holds content that is shared across multiple cuelsp
// client/editor connections.
type Cache struct {
	fs         *fscache.CUECacheFS
	registry   Registry
	extProfile *config.Profile
}

type Registry interface {
	modrequirements.Registry
	modpkgload.Registry
}
