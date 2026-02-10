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

package adt

import (
	"runtime"
	"sync"
	"weak"
)

// TODO: this was inspired by (but rewritten from) a suggestion in
// https://github.com/golang/go/issues/43615. Once this issue is resolved or a
// properly licensed package is released, we should consider using that.

// newMemoizer returns a new memoizer value that caches
// the results of calling the make function.
// It does not guarantee that there will be at most one
// *V value at any one time or that make won't be invoked concurrently.
//
// It does not memoize results when make returns an error,
func newMemoizer[K comparable, V any](make func(K) (*V, error)) *memoizer[K, V] {
	return &memoizer[K, V]{
		make: make,
	}
}

// memoizer implements a garbage-collectable cache of
// results from calling the make function.
type memoizer[K comparable, V any] struct {
	// make returns a new result for K. It is expected
	// that it will always return an equivalent non-nil value
	// for a given key.
	make func(K) (*V, error)
	// string -> weak.Pointer[V]
	m sync.Map
}

// get returns the result for the key k.
func (c *memoizer[K, V]) get(k K) (*V, error) {
	if entry, ok := c.m.Load(k); ok {
		if v := entry.(weak.Pointer[V]).Value(); v != nil {
			return v, nil
		}
	}
	// Could potentially use singleflight or similar to
	// avoid redundant make calls in concurrent situations
	// but the redundancy probably isn't much of an issue
	// in practice.
	v, err := c.make(k)
	if err != nil {
		return nil, err
	}
	wp := weak.Make(v)
	runtime.AddCleanup(v, func(wp weak.Pointer[V]) {
		c.m.CompareAndDelete(k, wp)
	}, wp)
	c.m.Store(k, wp)
	return v, nil
}
