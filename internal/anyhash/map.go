// Copyright 2025 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package anyhash defines hash-based containers such as [Map].
// It's a clone of the up-coming functionality in the Go stdlib,
// taken from https://go-review.googlesource.com/c/go/+/612217/30
//
// TODO change to use container/hash when it has landed in a Go version we're allowed
// to depend on.
package anyhash

import (
	"hash/maphash"
	"iter"
	"sync"

	"cuelang.org/go/internal/anyunique"
)

// Map[K, V] is a hash-table based mapping from keys K to values V,
// using the hash function and key-equivalence relation specified by
// at construction.
//
// A Map must be created with [NewMap].
// Neither a nil pointer nor the zero value are valid maps.
//
// Map values must not be copied; use [Map.Clone] instead.
//
// Do not use Map with a comparable key type K and its usual ==
// equivalence relation; instead, use Go's built-in map type.
// (If you need set operations such as Union and Intersection,
// use the helpers in the container/mapset package.)
type Map[K, V any] struct {
	seed   maphash.Seed
	table  map[uint64][]entry[K, V] // maps hash to entries of that hash
	len    int                      // number of map entries
	hasher anyunique.Hasher[K]

	_ noCopy
}

// NewMap returns a new map that uses the specified hash
// function and key-equivalence relation.
func NewMap[K, V any](hasher anyunique.Hasher[K]) *Map[K, V] {
	var m Map[K, V]
	m.init(hasher)
	return &m
}

type entry[K, V any] struct {
	used  bool
	key   K
	value V
}

func (m *Map[K, V]) init(hasher anyunique.Hasher[K]) {
	m.seed = maphash.MakeSeed()
	m.table = make(map[uint64][]entry[K, V])
	m.hasher = hasher
}

// hash returns the hash of the specified key.
func (m *Map[K, V]) hash(key K) uint64 {
	// Because the m.hasher.Hash(h) call is dynamic,
	// escape analysis is conservative, causing Hash h to escape.
	// To amortize allocation, we recycle Hashes using a sync.Pool.
	//
	// (Even if we had expressed the hasher type as an additional
	// type parameter, it would not have been sufficient to cause
	// escape analysis to stack-allocate h.)
	h := hashPool.Get().(*maphash.Hash)
	h.SetSeed(m.seed)
	m.hasher.Hash(h, key)
	sum := h.Sum64()
	hashPool.Put(h)
	return sum
}

var hashPool = sync.Pool{
	New: func() any { return new(maphash.Hash) },
}

// Len returns the number of map entries.
func (m *Map[K, V]) Len() int {
	return m.len
}

// All returns an iterator over the key/value entries of the map in
// unspecified order.
//
// It is safe to modify the map during iteration. As with the built-in
// map, if a key is removed during iteration, its entry will not be
// yielded by the iterator. If a key is added during iteration, it may
// or may not be yielded by the iterator. The iterator yields the
// value most recently associated with a given key.
func (m *Map[K, V]) All() iter.Seq2[K, V] {
	_ = m.len
	return func(yield func(K, V) bool) {
		// Iterate by hash key to dynamically evaluate bucket slice length.
		// This avoids holding a stale slice header if a concurrent or
		// nested Set reallocates the bucket, matching built-in map semantics.
		for hash := range m.table {
			for i := 0; i < len(m.table[hash]); i++ {
				e := &m.table[hash][i]
				if e.used && !yield(e.key, e.value) {
					return
				}
			}
		}
	}
}

// Clone returns a new non-nil map with the same entries as m.
func (m *Map[K, V]) Clone() *Map[K, V] {
	var copy Map[K, V]
	copy.cloneFrom(m)
	return &copy
}

func (m *Map[K, V]) cloneFrom(src *Map[K, V]) {
	*m = Map[K, V]{
		hasher: src.hasher,
		seed:   src.seed,
		table:  make(map[uint64][]entry[K, V]),
	}
	m.SetAll(src.All())
}

// Keys returns an iterator over the keys of the map in random order.
//
// It is safe to modify the map during iteration, with the same
// semantics as the [Map.All] iterator.
func (m *Map[K, V]) Keys() iter.Seq[K] {
	_ = m.len
	return func(yield func(K) bool) {
		// Iterate by hash key to dynamically evaluate bucket slice length.
		// This safely handles additions/reallocations during iteration.
		for hash := range m.table {
			for i := 0; i < len(m.table[hash]); i++ {
				e := &m.table[hash][i]
				if e.used && !yield(e.key) {
					return
				}
			}
		}
	}
}

// Values returns an iterator over the values of the map in random order.
//
// It is safe to modify the map during iteration, with the same
// semantics as the [Map.All] iterator.
func (m *Map[K, V]) Values() iter.Seq[V] {
	_ = m.len
	return func(yield func(V) bool) {
		// Iterate by hash key to dynamically evaluate bucket slice length.
		// This safely handles additions/reallocations during iteration.
		for hash := range m.table {
			for i := 0; i < len(m.table[hash]); i++ {
				e := &m.table[hash][i]
				if e.used && !yield(e.value) {
					return
				}
			}
		}
	}
}

// Delete removes the entry with the given key, if present.
// It reports whether the map changed, and returns the previous value, if any.
func (m *Map[K, V]) Delete(key K) (V, bool) {
	bucket := m.table[m.hash(key)]
	for i, e := range bucket {
		if e.used && m.hasher.Equal(key, e.key) {
			// We can't compact the bucket as it
			// would disturb iterators.
			prev := e.value
			bucket[i] = entry[K, V]{}
			m.len--
			return prev, true
		}
	}
	return *new(V), false
}

// DeleteAll removes all the entries whose keys are in the sequence.
// It reports whether the map changed.
func (m *Map[K, V]) DeleteAll(keys iter.Seq[K]) bool {
	changed := false
	for k := range keys {
		if _, ok := m.Delete(k); ok {
			changed = true
		}
	}
	return changed
}

// DeleteFunc removes each map entry for which del returns true.
// It reports whether the map changed.
func (m *Map[K, V]) DeleteFunc(del func(K, V) bool) bool {
	changed := false
	for k, v := range m.All() {
		if del(k, v) {
			if _, ok := m.Delete(k); ok {
				changed = true
			}
		}
	}
	return changed
}

// Get reports whether the map contains the specified key, and returns
// the corresponding value if found, or the zero value if not.
func (m *Map[K, V]) Get(key K) (V, bool) {
	_, v, ok := m.Get2(key)
	return v, ok
}

// Get2 is like [Map.Get] but also returns the actual key that's stored
// in the map.
// NOTE: this method was not present in the CL this code
// was taken from.
func (m *Map[K, V]) Get2(key K) (K, V, bool) {
	for _, e := range m.table[m.hash(key)] {
		if e.used && m.hasher.Equal(key, e.key) {
			return e.key, e.value, true
		}
	}
	return *new(K), *new(V), false
}

// At returns the value corresponding the given key,
// or the zero value if the entry is not present.
func (m *Map[K, V]) At(key K) V {
	v, _ := m.Get(key)
	return v
}

// Contains reports whether the map contains an entry with the given key.
func (m *Map[K, V]) Contains(key K) bool {
	_, ok := m.Get(key)
	return ok
}

// ContainsAll reports whether the map contains an entry for each key in the sequence.
func (m *Map[K, V]) ContainsAll(keys iter.Seq[K]) bool {
	return every(keys, m.Contains)
}

// Set updates the map entry for key to value,
// and returns the previous entry, if any.
// It reports whether the map size increased.
func (m *Map[K, V]) Set(key K, value V) (prev V, changed bool) {
	hash := m.hash(key)
	bucket := m.table[hash]
	var hole *entry[K, V]
	for i := range bucket {
		e := &bucket[i]
		if e.used {
			if m.hasher.Equal(key, e.key) {
				prev = e.value
				e.value = value
				return
			}
		} else if hole == nil {
			hole = e
		}
	}
	if hole != nil {
		*hole = entry[K, V]{true, key, value} // overwrite deleted entry
	} else {
		m.table[hash] = append(bucket, entry[K, V]{true, key, value})
	}

	m.len++
	changed = true
	return
}

// SetAll calls Set(k, v) for each key/value pair in the sequence.
// It reports whether the number of map entries increased.
func (m *Map[K, V]) SetAll(seq iter.Seq2[K, V]) bool {
	pre := m.len
	for k, v := range seq {
		m.Set(k, v)
	}
	return m.len > pre
}

// Clear removes all entries from the map.
func (m *Map[K, V]) Clear() {
	clear(m.table)
	m.len = 0
}

// noCopy may be added to structs which must not be copied
// after the first use.
//
// See https://golang.org/issues/8005#issuecomment-190753527
// for details.
//
// Note that it must not be embedded, due to the Lock and Unlock methods.
type noCopy struct{}

// Lock is a no-op used by -copylocks checker from `go vet`.
func (*noCopy) Lock()   {}
func (*noCopy) Unlock() {}

// every reports whether f(v) is true for every element in seq.
// It stops at the first element where f returns false.
// This is equivalent to the up-coming [iter.Seq.Every] method.
func every[V any](seq iter.Seq[V], f func(V) bool) bool {
	every := true
	seq(func(v V) bool {
		if !f(v) {
			every = false
			return false
		}
		return true
	})
	return every

	// for v := range seq {
	// 	if !f(v) {
	// 		return false
	// 	}
	// }
	// return true
}
