// Copyright 2025 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package anyhash_test

import (
	"hash/maphash"
	"testing"

	"cuelang.org/go/internal/anyhash"
)

func TestIterationNonDeterminism(t *testing.T) {
	m := anyhash.NewMap[int, int](ComparableHasher[int]{})
	const n = 100
	for i := range n {
		m.Set(i, i)
	}

	collect := func() []int {
		var s []int
		for k := range m.Keys() {
			s = append(s, k)
		}
		return s
	}

	first := collect()
	if len(first) != n {
		t.Fatalf("collected %d items, want %d", len(first), n)
	}

	// Repeatedly collect sequences until we find one that differs
	// from the first. With Go's map randomization, this should
	// happen almost immediately for a map of size 100.
	const maxAttempts = 10
	for i := range maxAttempts {
		second := collect()
		if len(first) != len(second) {
			t.Fatalf("attempt %d: lengths differ: %d != %d", i, len(first), len(second))
		}
		for j := range first {
			if first[j] != second[j] {
				return // Found a different sequence, success.
			}
		}
	}

	t.Errorf("iteration order was deterministic over %d iterations of %d elements", maxAttempts, n)
}

func TestIterationMutation(t *testing.T) {
	m := anyhash.NewMap[int, int](ComparableHasher[int]{})
	const n = 100
	for i := range n {
		m.Set(i, i*10)
	}

	// Ensure that mutation during iteration doesn't crash and
	// generally follows the promised semantics.
	count := 0
	for k := range m.Keys() {
		if k == 50 {
			// Add 1000 new items to trigger potential reallocations.
			for j := 0; j < 1000; j++ {
				m.Set(n+j, j)
			}
			// Delete some existing items.
			for j := 0; j < n; j += 2 {
				m.Delete(j)
			}
		}
		count++
	}
	// The exact count depends on the iteration order, but it
	// should be at least some reasonable number.
	if count < n/2 {
		t.Errorf("iteration ended early: count=%d", count)
	}
}

type ComparableHasher[T comparable] struct {
	_ [0]func(T) // disallow comparison, and conversion between ComparableHasher[X] and ComparableHasher[Y]
}

func (ComparableHasher[T]) Hash(h *maphash.Hash, v T) { maphash.WriteComparable(h, v) }
func (ComparableHasher[T]) Equal(x, y T) bool         { return x == y }
