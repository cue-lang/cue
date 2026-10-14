// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package par implements parallel execution helpers.
package par

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
)

// ErrCache is like Cache except that it also stores
// an error value alongside the cached value V.
type ErrCache[K comparable, V any] struct {
	Cache[K, errValue[V]]
}

type errValue[V any] struct {
	v   V
	err error

	// canceled is set if the result was not cached; see [ErrCache.DoContext].
	canceled bool
}

func (c *ErrCache[K, V]) Do(key K, f func() (V, error)) (V, error) {
	v := c.Cache.Do(key, func() errValue[V] {
		v, err := f()
		return errValue[V]{v: v, err: err}
	})
	return v.v, v.err
}

// DoContext is like [ErrCache.Do], except that a failure of f while ctx is
// canceled is not cached. As ctx belongs to the one caller whose f ran,
// callers waiting on the same key, and later callers, call their own f.
func (c *ErrCache[K, V]) DoContext(ctx context.Context, key K, f func() (V, error)) (V, error) {
	for {
		v := c.Cache.Do(key, func() errValue[V] {
			v, err := f()
			canceled := err != nil && ctx.Err() != nil
			if canceled {
				// Forget the entry before Do marks it as done,
				// so that only the callers already waiting see it.
				c.m.Delete(key)
			}
			return errValue[V]{v, err, canceled}
		})
		if !v.canceled || ctx.Err() != nil {
			return v.v, v.err
		}
	}
}

var ErrCacheEntryNotFound = errors.New("cache entry not found")

// Get returns the cached result associated with key.
// It returns ErrCacheEntryNotFound if there is no such result.
func (c *ErrCache[K, V]) Get(key K) (V, error) {
	v, ok := c.Cache.Get(key)
	if !ok {
		v.err = ErrCacheEntryNotFound
	}
	return v.v, v.err
}

// Cache runs an action once per key and caches the result.
type Cache[K comparable, V any] struct {
	m sync.Map
}

type cacheEntry[V any] struct {
	done   atomic.Bool
	mu     sync.Mutex
	result V
}

// Do calls the function f if and only if Do is being called for the first time with this key.
// No call to Do with a given key returns until the one call to f returns.
// Do returns the value returned by the one call to f.
func (c *Cache[K, V]) Do(key K, f func() V) V {
	entryIface, ok := c.m.Load(key)
	if !ok {
		entryIface, _ = c.m.LoadOrStore(key, new(cacheEntry[V]))
	}
	e := entryIface.(*cacheEntry[V])
	if !e.done.Load() {
		e.mu.Lock()
		if !e.done.Load() {
			e.result = f()
			e.done.Store(true)
		}
		e.mu.Unlock()
	}
	return e.result
}

// Get returns the cached result associated with key
// and reports whether there is such a result.
//
// If the result for key is being computed, Get does not wait for the computation to finish.
func (c *Cache[K, V]) Get(key K) (V, bool) {
	entryIface, ok := c.m.Load(key)
	if !ok {
		return *new(V), false
	}
	e := entryIface.(*cacheEntry[V])
	if !e.done.Load() {
		return *new(V), false
	}
	return e.result, true
}
