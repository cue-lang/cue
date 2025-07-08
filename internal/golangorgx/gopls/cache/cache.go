// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import (
	"strconv"
	"sync/atomic"

	"cuelang.org/go/internal/lsp/fscache"
	"cuelang.org/go/internal/mod/modpkgload"
	"cuelang.org/go/internal/mod/modrequirements"
	"cuelang.org/go/mod/modconfig"
)

func NewCache(registry Registry) (*Cache, error) {
	counter := atomic.AddInt64(&cacheIDCounter, 1)

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
		id:       strconv.FormatInt(counter, 10),
		fs:       fscache.NewCUECachedFS(),
		registry: registry,
	}, nil
}

// A Cache holds content that is shared across multiple gopls sessions.
type Cache struct {
	id string

	fs       *fscache.CUECacheFS
	registry Registry
}

var cacheIDCounter int64

func (c *Cache) ID() string { return c.id }

type Registry interface {
	modrequirements.Registry
	modpkgload.Registry
}
