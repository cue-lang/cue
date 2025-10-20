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

package anyunique_test

import (
	"hash/maphash"
	"slices"
	"testing"

	"cuelang.org/go/internal/anyunique"
	"github.com/go-quicktest/qt"
)

// stringHasher is a test Hasher implementation for string values
// using standard equality.
type stringHasher struct{}

func (stringHasher) Equal(a, b string) bool {
	return a == b
}

func (stringHasher) Hash(h *maphash.Hash, s string) {
	h.WriteString(s)
}

// simpleIntHasher is a test Hasher implementation for int values.
type simpleIntHasher struct{}

func (simpleIntHasher) Equal(a, b int) bool {
	return a == b
}

func (simpleIntHasher) Hash(h *maphash.Hash, v int) {
	maphash.WriteComparable(h, v)
}

// node represents a struct pointer containing a slice - a common pattern
// that needs custom equality/hashing since the pointer itself isn't enough.
type node struct {
	values []int
}

// nodeHasher implements deep equality for node pointers.
type nodeHasher struct{}

func (nodeHasher) Equal(a, b *node) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return slices.Equal(a.values, b.values)
}

func (nodeHasher) Hash(h *maphash.Hash, n *node) {
	if n == nil {
		return
	}
	for _, v := range n.values {
		maphash.WriteComparable(h, v)
	}
}

func TestNew(t *testing.T) {
	s := anyunique.New[string](stringHasher{})
	qt.Assert(t, qt.Not(qt.IsNil(s)))
}

func TestStore_Make_BasicEquality(t *testing.T) {
	s := anyunique.New[string](stringHasher{})

	// First call creates a new unique value
	u1 := s.Make("hello")
	qt.Assert(t, qt.Equals(u1.Get(), "hello"))

	// Second call with same value should return equal U[T]
	u2 := s.Make("hello")
	qt.Assert(t, qt.Equals(u2.Get(), "hello"))
	qt.Assert(t, qt.Equals(u1, u2))

	// Different value should not be equal
	u3 := s.Make("world")
	qt.Assert(t, qt.Equals(u3.Get(), "world"))
	qt.Assert(t, qt.Not(qt.Equals(u1, u3)))
}

func TestStore_Make_ZeroValue(t *testing.T) {
	s := anyunique.New[string](stringHasher{})

	// Zero value should be handled specially
	u1 := s.Make("")
	u2 := s.Make("")
	qt.Assert(t, qt.Equals(u1.Get(), ""))
	qt.Assert(t, qt.Equals(u2.Get(), ""))
	qt.Assert(t, qt.Equals(u1, u2))

	// Zero value should not equal non-zero
	u3 := s.Make("something")
	qt.Assert(t, qt.Not(qt.Equals(u1, u3)))
}

func TestStore_Make_StructPointers(t *testing.T) {
	s := anyunique.New[*node](nodeHasher{})

	node1 := &node{values: []int{1, 2, 3}}
	node2 := &node{values: []int{1, 2, 3}} // same content, different pointer
	node3 := &node{values: []int{1, 2, 4}} // different content

	u1 := s.Make(node1)
	u2 := s.Make(node2)
	u3 := s.Make(node3)

	// u1 and u2 should be equal (same content, even though different pointers)
	qt.Assert(t, qt.Equals(u1, u2))
	qt.Assert(t, qt.DeepEquals(u1.Get().values, []int{1, 2, 3}))
	qt.Assert(t, qt.DeepEquals(u2.Get().values, []int{1, 2, 3}))

	// The stored pointer should be the first one encountered
	qt.Assert(t, qt.Equals(u1.Get(), node1))
	qt.Assert(t, qt.Equals(u2.Get(), node1)) // canonicalized to node1

	// u3 should be different
	qt.Assert(t, qt.Not(qt.Equals(u1, u3)))
	qt.Assert(t, qt.DeepEquals(u3.Get().values, []int{1, 2, 4}))
	qt.Assert(t, qt.Equals(u3.Get(), node3))
}

func TestStore_Make_IntValues(t *testing.T) {
	s := anyunique.New[int](simpleIntHasher{})

	u1 := s.Make(42)
	u2 := s.Make(42)
	u3 := s.Make(100)

	qt.Assert(t, qt.Equals(u1, u2))
	qt.Assert(t, qt.Not(qt.Equals(u1, u3)))
	qt.Assert(t, qt.Equals(u1.Get(), 42))
	qt.Assert(t, qt.Equals(u3.Get(), 100))
}

func TestStore_Make_MultipleValues(t *testing.T) {
	s := anyunique.New[string](stringHasher{})

	values := []string{"apple", "banana", "cherry", "date", "elderberry"}
	uniqueValues := make(map[anyunique.U[string]]bool)

	// Create unique values for all strings
	for _, v := range values {
		u := s.Make(v)
		uniqueValues[u] = true
		qt.Assert(t, qt.Equals(u.Get(), v))
	}

	// Should have 5 distinct unique values
	qt.Assert(t, qt.Equals(len(uniqueValues), 5))

	// Re-making the same values should return equal U[T]s
	for _, v := range values {
		u := s.Make(v)
		qt.Assert(t, qt.Equals(uniqueValues[u], true))
	}
}

// badHasher creates intentional hash collisions for testing.
type badHasher struct{}

func (badHasher) Equal(a, b string) bool {
	return a == b
}

func (badHasher) Hash(*maphash.Hash, string) {
	// Don't write anything, so we always get the same hash
}

func TestStore_Make_HashCollisions(t *testing.T) {
	s := anyunique.New[string](badHasher{})

	// All these will hash to the same bucket
	u1 := s.Make("key1")
	u2 := s.Make("key2")
	u3 := s.Make("key3")

	// They should all be different despite hash collision
	qt.Assert(t, qt.Not(qt.Equals(u1, u2)))
	qt.Assert(t, qt.Not(qt.Equals(u2, u3)))
	qt.Assert(t, qt.Not(qt.Equals(u1, u3)))

	// But re-making should return equal values
	u1b := s.Make("key1")
	u2b := s.Make("key2")
	u3b := s.Make("key3")

	qt.Assert(t, qt.Equals(u1, u1b))
	qt.Assert(t, qt.Equals(u2, u2b))
	qt.Assert(t, qt.Equals(u3, u3b))
}

func TestStore_WriteHash(t *testing.T) {
	s := anyunique.New[string](stringHasher{})

	u1 := s.Make("hello")
	u2 := s.Make("hello")
	u3 := s.Make("world")

	// Write hashes for equal values
	var h1, h2 maphash.Hash
	h1.SetSeed(maphash.MakeSeed())
	h2.SetSeed(h1.Seed())

	s.WriteHash(&h1, u1)
	s.WriteHash(&h2, u2)

	// Hashes of equal values should be the same
	qt.Assert(t, qt.Equals(h1.Sum64(), h2.Sum64()))

	// Hash of different value should be different
	var h3 maphash.Hash
	h3.SetSeed(h1.Seed())
	s.WriteHash(&h3, u3)

	qt.Assert(t, qt.Not(qt.Equals(h1.Sum64(), h3.Sum64())))
}

func TestStore_WriteHash_ZeroValue(t *testing.T) {
	s := anyunique.New[string](stringHasher{})

	u1 := s.Make("")
	u2 := s.Make("")

	var h1, h2 maphash.Hash
	seed := maphash.MakeSeed()
	h1.SetSeed(seed)
	h2.SetSeed(seed)

	s.WriteHash(&h1, u1)
	s.WriteHash(&h2, u2)

	// Zero values should hash the same
	qt.Assert(t, qt.Equals(h1.Sum64(), h2.Sum64()))
}

func TestU_Get(t *testing.T) {
	s := anyunique.New[string](stringHasher{})

	u := s.Make("test")
	qt.Assert(t, qt.Equals(u.Get(), "test"))

	// Zero value
	var zeroU anyunique.U[string]
	qt.Assert(t, qt.Equals(zeroU.Get(), ""))
}

func TestStore_Make_NilPointers(t *testing.T) {
	s := anyunique.New[*node](nodeHasher{})

	u1 := s.Make(nil)
	u2 := s.Make(nil)

	// Nil pointers should be treated as zero values
	qt.Assert(t, qt.Equals(u1, u2))
	qt.Assert(t, qt.IsNil(u1.Get()))
	qt.Assert(t, qt.IsNil(u2.Get()))

	// Non-nil should be different from nil
	u3 := s.Make(&node{values: []int{1}})
	qt.Assert(t, qt.Not(qt.Equals(u1, u3)))
}

func TestStore_Make_RepeatedCalls(t *testing.T) {
	s := anyunique.New[string](stringHasher{})

	// Make the same value many times
	var values []anyunique.U[string]
	for i := 0; i < 100; i++ {
		values = append(values, s.Make("repeated"))
	}

	// All should be equal
	first := values[0]
	for _, v := range values[1:] {
		qt.Assert(t, qt.Equals(v, first))
	}
}

func TestStore_Make_DifferentStores(t *testing.T) {
	s1 := anyunique.New[string](stringHasher{})
	s2 := anyunique.New[string](stringHasher{})

	u1 := s1.Make("hello")
	u2 := s2.Make("hello")

	// Values from different stores should not be comparable in practice,
	// but technically they might compare equal if the underlying strings
	// are the same. The important thing is they're independent stores.
	qt.Assert(t, qt.Equals(u1.Get(), "hello"))
	qt.Assert(t, qt.Equals(u2.Get(), "hello"))

	// Hashes from different stores will be different due to different seeds
	var h1, h2 maphash.Hash
	h1.SetSeed(maphash.MakeSeed())
	h2.SetSeed(maphash.MakeSeed())

	s1.WriteHash(&h1, u1)
	s2.WriteHash(&h2, u2)

	// Very unlikely to be equal with different seeds
	// (This test might occasionally fail due to hash collision, but extremely rare)
}

func TestStore_WriteHash_ConsistentWithMake(t *testing.T) {
	s := anyunique.New[string](stringHasher{})

	// Create several unique values
	values := []string{"alpha", "beta", "gamma"}
	uniques := make([]anyunique.U[string], len(values))
	for i, v := range values {
		uniques[i] = s.Make(v)
	}

	// The hash of the unique value should be consistent
	// i.e., calling WriteHash multiple times should give the same result
	for _, u := range uniques {
		var h1, h2 maphash.Hash
		seed := maphash.MakeSeed()
		h1.SetSeed(seed)
		h2.SetSeed(seed)

		s.WriteHash(&h1, u)
		s.WriteHash(&h2, u)

		qt.Assert(t, qt.Equals(h1.Sum64(), h2.Sum64()))
	}
}

func TestStore_Make_StressTest(t *testing.T) {
	s := anyunique.New[int](simpleIntHasher{})

	// Create many unique values
	n := 10000
	uniqueMap := make(map[int]anyunique.U[int])

	for i := 0; i < n; i++ {
		u := s.Make(i)
		uniqueMap[i] = u
		qt.Assert(t, qt.Equals(u.Get(), i))
	}

	// Verify all values are stored correctly and re-making gives equal results
	for i := 0; i < n; i++ {
		u := s.Make(i)
		qt.Assert(t, qt.Equals(u, uniqueMap[i]))
	}
}

// treeNode represents a tree structure with unique child pointers
type treeNode struct {
	value    int
	children []*treeNode
}

// treeHasher implements deep equality for tree nodes
type treeHasher struct {
	childStore *anyunique.Store[*treeNode, treeHasher]
}

func (h treeHasher) Equal(a, b *treeNode) bool {
	if a == b {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.value != b.value || len(a.children) != len(b.children) {
		return false
	}
	for i := range a.children {
		if !h.Equal(a.children[i], b.children[i]) {
			return false
		}
	}
	return true
}

func (h treeHasher) Hash(hash *maphash.Hash, n *treeNode) {
	if n == nil {
		return
	}
	maphash.WriteComparable(hash, n.value)
	maphash.WriteComparable(hash, len(n.children))
	for _, child := range n.children {
		h.Hash(hash, child)
	}
}

func TestStore_Make_NestedStructures(t *testing.T) {
	s := anyunique.New[*treeNode](treeHasher{})

	// Create identical tree structures with different pointers
	tree1 := &treeNode{
		value: 1,
		children: []*treeNode{
			{value: 2},
			{value: 3},
		},
	}

	tree2 := &treeNode{
		value: 1,
		children: []*treeNode{
			{value: 2},
			{value: 3},
		},
	}

	tree3 := &treeNode{
		value: 1,
		children: []*treeNode{
			{value: 2},
			{value: 4}, // different
		},
	}

	u1 := s.Make(tree1)
	u2 := s.Make(tree2)
	u3 := s.Make(tree3)

	// tree1 and tree2 have same structure, should be equal
	qt.Assert(t, qt.Equals(u1, u2))
	qt.Assert(t, qt.Equals(u1.Get(), tree1)) // canonicalized to tree1

	// tree3 is different
	qt.Assert(t, qt.Not(qt.Equals(u1, u3)))
}

func TestStore_Make_ZeroValueInt(t *testing.T) {
	s := anyunique.New[int](simpleIntHasher{})

	u1 := s.Make(0)
	u2 := s.Make(0)

	// Zero value for int is 0
	qt.Assert(t, qt.Equals(u1, u2))
	qt.Assert(t, qt.Equals(u1.Get(), 0))
}

func TestStore_WriteHash_DifferentSeeds(t *testing.T) {
	s := anyunique.New[string](stringHasher{})

	u := s.Make("test")

	// Different seeds should produce different hashes
	var h1, h2 maphash.Hash
	h1.SetSeed(maphash.MakeSeed())
	h2.SetSeed(maphash.MakeSeed())

	s.WriteHash(&h1, u)
	s.WriteHash(&h2, u)

	// With extremely high probability, different seeds give different hashes
	// Note: This could theoretically fail, but it's astronomically unlikely
}

func TestStore_Make_LargeStructPointers(t *testing.T) {
	s := anyunique.New[*node](nodeHasher{})

	// Create nodes with large slices
	largeSlice := make([]int, 1000)
	for i := range largeSlice {
		largeSlice[i] = i
	}

	node1 := &node{values: largeSlice}
	node2 := &node{values: append([]int{}, largeSlice...)} // copy

	u1 := s.Make(node1)
	u2 := s.Make(node2)

	// Should be equal despite different underlying pointers
	qt.Assert(t, qt.Equals(u1, u2))
	qt.Assert(t, qt.Equals(u1.Get(), node1))
}

func TestStore_Make_AlternatingPatterns(t *testing.T) {
	s := anyunique.New[string](stringHasher{})

	// Alternate between two values many times
	for i := 0; i < 100; i++ {
		u1 := s.Make("a")
		u2 := s.Make("b")
		u3 := s.Make("a")

		qt.Assert(t, qt.Equals(u1, u3))
		qt.Assert(t, qt.Not(qt.Equals(u1, u2)))
	}
}

func TestStore_WriteHash_MultipleValues(t *testing.T) {
	s := anyunique.New[string](stringHasher{})

	values := []string{"apple", "banana", "cherry", "date"}
	uniques := make([]anyunique.U[string], len(values))

	for i, v := range values {
		uniques[i] = s.Make(v)
	}

	// All unique values should have different hashes
	hashes := make(map[uint64]bool)
	seed := maphash.MakeSeed()

	for _, u := range uniques {
		var h maphash.Hash
		h.SetSeed(seed)
		s.WriteHash(&h, u)
		hash := h.Sum64()

		// Each should be unique (no collisions expected for these values)
		qt.Assert(t, qt.Equals(hashes[hash], false))
		hashes[hash] = true
	}
}
