// Copyright 2019 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package cache

import (
	"strconv"
	"sync/atomic"

	"cuelang.org/go/internal/golangorgx/tools/memoize"
)

// New Creates a new cache for gopls operation results, using the given file
// set, shared store, and session options.
//
// Both the fset and store may be nil, but if store is non-nil so must be fset
// (and they must always be used together), otherwise it may be possible to get
// cached data referencing token.Pos values not mapped by the FileSet.
func New() *Cache {
	index := atomic.AddInt64(&cacheIndex, 1)

	c := &Cache{
		id: strconv.FormatInt(index, 10),
	}
	return c
}

// A Cache holds content that is shared across multiple gopls sessions.
type Cache struct {
	id string

	// store holds cached calculations.
	//
	// TODO(rfindley): at this point, these are not important, as we've moved our
	// content-addressable cache to the file system (the filecache package). It
	// is unlikely that this shared cache provides any shared value. We should
	// consider removing it, replacing current uses with a simpler futures cache,
	// as we've done for e.g. type-checked packages.
	store *memoize.Store
}

var cacheIndex, sessionIndex, viewIndex int64

func (c *Cache) ID() string { return c.id }
