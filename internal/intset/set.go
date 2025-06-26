// Package u32set provides an allocation-efficient hash set for any type whose
// **underlying type** is uint32 (e.g. `type ID uint32`).  This generic version
// keeps the keys in a `[]T` slice – not `[]uint32` – eliminating the internal
// round-trip conversions while retaining the exact same memory footprint.
//
// Features
//   - O(1) Add and Has (open addressing with linear probing)
//   - O(1) Clear with zero allocations via generation counting
//   - Automatic re-hashing when load factor > 75 %
//   - Minimal garbage: only the backing slices are ever (re)allocated
//
// Usage example:
//
//	type ID uint32
//	var s u32set.Set[ID]
//	s.Init(128)
//	s.Add(ID(42))
//	fmt.Println(s.Has(ID(42))) // true
//
// NOTE: The implementation is **not** safe for concurrent use without external
// synchronisation.
//
// Author: Rog (2025-06-26)
// License: Apache 2.0 – do what you like, no warranty.
package intset

import "math/bits"

type Int interface {
	~uint8 | ~uint16 | ~uint32 | ~uint64
}

// Set is a high-performance hash set for types whose underlying type is uint32.
//
// All methods have pointer receivers; copying the *Set value itself is shallow –
// only slice headers are copied.  The zero value is NOT ready for use; call
// Init or New first.
//
// T must satisfy ~uint32, meaning any defined type whose underlying type is
// uint32 (including plain uint32 itself).
//
// Internally we store keys directly as []T.  We still convert individual keys
// to uint32 when hashing/probing, which is a single machine instruction and
// imposes no allocation.
//
// Big-O performance:
//
//	Add      – O(1) amortised
//	Has      – O(1) expected
//	Clear    – O(1)
//	Rehash   – O(n) (rare, when growing)
//
// Memory: 8 bytes per slot (key + generation).  A 1-million-element set uses ≈8 MiB.
//
// Generation overflow: gen is uint32 and wraps every ≈4 billion Clear calls.  On
// wrap we zero the generations slice to avoid false positives.
//
// Concurrency: NOT safe without external locking (e.g. sync.Mutex).
//
//go:generate go vet ./...
type Set[T Int] struct {
	keys        []T      // stored as the concrete generic type
	generations []uint32 // parallel generation numbers
	gen         uint32   // current generation (never zero)
	size        int      // live keys in current generation
	maxLoad     float32  // load-factor threshold (capacity*maxLoad)
}

// New returns a pointer to an initialised Set with at least capacity slots.
// Capacity is rounded up to the next power of two and min 8.
func New[T ~uint32](capacity int) *Set[T] {
	var s Set[T]
	s.Init(capacity)
	return &s
}

// Init (re)initialises s with at least capacity slots. Existing memory is
// reused when possible. The method is idempotent.
func (s *Set[T]) Init(capacity int) {
	if capacity < 8 {
		capacity = 8
	}
	capPow := nextPow2(uint32(capacity))
	if len(s.keys) != int(capPow) {
		s.keys = make([]T, capPow)
		s.generations = make([]uint32, capPow)
	}
	s.gen = 1
	s.size = 0
	s.maxLoad = 0.75 // 75 % load factor
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
	k := uint32(x)
	mask := uint32(len(s.keys) - 1)
	i := k & mask
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
	if len(s.keys) == 0 {
		s.Init(8)
	}
	if float32(s.size+1) > float32(len(s.keys))*s.maxLoad {
		s.rehash(len(s.keys) * 2)
	}
	k := uint32(x)
	mask := uint32(len(s.keys) - 1)
	i := k & mask
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

	mask := uint32(newCap - 1)
	for idx, g := range oldGens {
		if g != oldGen {
			continue
		}
		kT := oldKeys[idx]
		k := uint32(kT)
		j := k & mask
		for {
			if s.generations[j] != s.gen {
				s.keys[j] = kT
				s.generations[j] = s.gen
				s.size++
				break
			}
			j = (j + 1) & mask
		}
	}
}

// nextPow2 returns the next power of two ≥ v.
func nextPow2(v uint32) uint32 {
	if v == 0 {
		return 1
	}
	if bits.OnesCount32(v) == 1 {
		return v
	}
	return 1 << (32 - bits.LeadingZeros32(v))
}
