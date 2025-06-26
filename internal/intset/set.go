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

// Package intset provides an allocation-efficient hash set for unsigned
// integer types. It does not provide a way to delete keys, and will not
// be efficient when most of the variation in keys is in the higher bits,
// because it uses masking rather than modulus to calculate hash slots.
//
// It's efficient to clear, working around https://github.com/golang/go/issues/70617
//
// Usage example:
//
//	type ID uint32
//	s := intset.New[ID](128)
//	s.Add(ID(42))
//	fmt.Println(s.Has(ID(42))) // true
package intset

import (
	"fmt"
	"math/bits"
)

// Int holds the possible integer types that the set can hold.
type Int interface {
	~uint8 | ~uint16 | ~uint32 | ~uint64
}

// Set is a high-performance hash set for unsigned integer types.
type Set[T Int] struct {
	keys        []T      // stored as the concrete generic type
	generations []uint32 // parallel generation numbers
	gen         uint32   // current generation (never zero)
	size        int      // live keys in current generation
	maxLoad     int      // threshold for rehashing
}

const maxLoadFactor = 0.75 // 75% load factor before resizing

// New returns a pointer to an initialised Set with at least capacity slots.
// Capacity is rounded up to the next power of two and min 8.
func New[T Int](capacity int) *Set[T] {
	capacity = nextPow2(max(capacity, 8))
	return &Set[T]{
		keys:        make([]T, capacity),
		generations: make([]uint32, capacity),
		gen:         1,
		maxLoad:     int(float64(capacity) * maxLoadFactor),
	}
}

// Clear discards all keys in O(1) without allocating.
func (s *Set[T]) Clear() {
	if len(s.keys) == 0 {
		return
	}
	s.gen++
	s.size = 0
	if s.gen == 0 { // wrapped – zero gens slice
		s.gen = 1
		for i := range s.generations {
			s.generations[i] = 0
		}
	}
}

// Len returns the number of keys currently in the set.
func (s *Set[T]) Len() int { return s.size }

// Has reports whether x is present.
func (s *Set[T]) Has(x T) bool {
	if len(s.keys) == 0 {
		return false
	}
	mask := len(s.keys) - 1
	i := int(x & T(mask))
	for {
		if s.generations[i] != s.gen {
			return false
		}
		if s.keys[i] == x {
			return true
		}
		i = (i + 1) & mask
	}
}

// Add inserts x and returns true if it was newly added.
func (s *Set[T]) Add(x T) bool {
	if s.size+1 >= s.maxLoad {
		s.rehash(len(s.keys) * 2)
	}
	mask := len(s.keys) - 1
	i := int(x & T(mask))
	for {
		if s.generations[i] != s.gen {
			s.keys[i] = x
			s.generations[i] = s.gen
			s.size++
			return true
		}
		if s.keys[i] == x {
			return false
		}
		i = (i + 1) & mask
	}
}

// rehash grows the table to newCap (must be a power of two) and reinserts live keys.
func (s *Set[T]) rehash(newCap int) {
	oldKeys := s.keys
	oldGens := s.generations
	oldGen := s.gen

	s.keys = make([]T, newCap)
	s.generations = make([]uint32, newCap)
	s.gen = 1
	s.size = 0
	s.maxLoad = int(float64(newCap) * maxLoadFactor)

	mask := newCap - 1
	for idx, g := range oldGens {
		if g != oldGen {
			continue
		}
		k := oldKeys[idx]
		j := int(k & T(mask))
		for {
			if s.generations[j] != s.gen {
				s.keys[j] = k
				s.generations[j] = s.gen
				s.size++
				break
			}
			j = (j + 1) & mask
		}
	}
}

// nextPow2 returns the next power of two ≥ v.
func nextPow2(v int) int {
	if v == 0 {
		return 1
	}
	if bits.OnesCount(uint(v)) == 1 {
		return v
	}
	n := bits.UintSize - bits.LeadingZeros(uint(v))
	if n < 0 {
		panic(fmt.Errorf("negative shift on nextPow2(%d): %d", v, n))
	}
	return 1 << (bits.UintSize - bits.LeadingZeros(uint(v)))
}
