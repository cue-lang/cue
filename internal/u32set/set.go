// Package u32set provides an allocation-efficient hash set for uint32 keys.
//
// Features
//   - O(1) Add and Has (open addressing with linear probing)
//   - O(1) Clear with zero allocations via generation counting
//   - Automatic re-hashing when the load factor exceeds 75 %
//   - Minimal garbage: only the backing slices are ever (re)allocated
//
// NOTE: The implementation is **not** safe for concurrent use without external
// synchronisation.
package u32set

// Set is a hash set for uint32 keys.
//
// The zero value is **not** immediately ready for use; call Init or New first.
// All methods have pointer receivers so a *Set may be copied around safely
// (only the slice headers are copied).
//
// The implementation uses open addressing with linear probing.  Each slot has
// a parallel generation number.  Calling Clear increments the generation so
// all existing entries become logically empty without the need to walk the
// table or zero memory.
//
// Rehashing allocates new backing slices and reinserts the live keys in a
// cache‑friendly way.  Because we know all live keys share the current
// generation, we can re‑insert them without tombstones.
//
// Big‑O performance:
//   Add      – O(1) amortised
//   Has      – O(1) expected
//   Clear    – O(1)
//   Rehash   – O(n) (rare, when growing)
//
// Memory:  (sizeof(uint32)+sizeof(uint32)) * capacity  == 8 bytes/slot.
// A 1 million‑element set takes ~8 MiB.
//
// Generation overflow: gen is uint32 and wraps roughly every 4 billion Clear
// calls.  When wrap is detected we zero the generations slice to avoid false
// positives.
//
// This implementation deliberately keeps the code simple and branch‑light; it
// can be made faster with quadratic probing or SIMD, but is typically on par
// with a map[uint32]struct{} while clearing orders of magnitude faster.
//
// Author: Rog (2025‑06‑26)
// License: Apache 2.0 (same as stdlib) – do what you like, no warranty.
import "math/bits"

// Set implements a high‑performance uint32 hash set.
// All methods have pointer receivers; copy the pointer, not the value.
//
// You MUST initialise the set via New or (*Set).Init before use.
type Set struct {
	keys        []uint32 // key storage
	generations []uint32 // parallel generation numbers
	gen         uint32   // current generation (never zero)
	size        int      // number of live keys in current generation
	maxLoad     float32  // load‑factor threshold (capacity*maxLoad)
}

// New returns a new *Set with capacity slots (rounded up to a power of two).
// If capacity <= 0 it defaults to 8.
func New(capacity int) *Set {
	var s Set
	s.Init(capacity)
	return &s
}

// Init (re)initialises s with at least capacity slots.
// Existing memory is reused when possible.
// The method is idempotent and can be called multiple times.
func (s *Set) Init(capacity int) {
	if capacity < 8 {
		capacity = 8
	}
	capPow := nextPow2(uint32(capacity))

	// (Re)allocate only if we do not already own slices of the right size.
	if len(s.keys) != int(capPow) {
		s.keys = make([]uint32, capPow)
		s.generations = make([]uint32, capPow)
	}
	s.gen = 1
	s.size = 0
	s.maxLoad = 0.75 // 75 % load‑factor threshold
}

// Clear discards all keys in O(1) without allocating.
func (s *Set) Clear() {
	if len(s.keys) == 0 {
		return // nothing to do
	}
	s.gen++
	s.size = 0

	// Handle wrap‑around: when gen wraps to 0 we must zero generations
	// or we'll keep seeing old entries as live.
	if s.gen == 0 {
		s.gen = 1
		for i := range s.generations {
			s.generations[i] = 0
		}
	}
}

// Len returns the number of keys currently in the set.
func (s *Set) Len() int { return s.size }

// Has reports whether x is present in the set.
func (s *Set) Has(x uint32) bool {
	if len(s.keys) == 0 {
		return false
	}
	mask := uint32(len(s.keys) - 1)
	i := x & mask // fast power‑of‑two hash bucket
	for {
		if s.generations[i] != s.gen {
			return false // empty slot in current generation
		}
		if s.keys[i] == x {
			return true
		}
		i = (i + 1) & mask // linear probing
	}
}

// Add inserts x into the set.  It returns true if x was newly added, false if
// it was already present.
func (s *Set) Add(x uint32) bool {
	if len(s.keys) == 0 {
		s.Init(8)
	}

	// Grow if load‑factor will exceed threshold
	if float32(s.size+1) > float32(len(s.keys))*s.maxLoad {
		s.rehash(len(s.keys) * 2)
	}

	mask := uint32(len(s.keys) - 1)
	i := x & mask
	for {
		if s.generations[i] != s.gen {
			// Free slot in this generation – insert.
			s.keys[i] = x
			s.generations[i] = s.gen
			s.size++
			return true
		}
		if s.keys[i] == x {
			return false // already present
		}
		i = (i + 1) & mask
	}
}

// rehash grows the table to newCap (must be a power of two) and reinserts live keys.
func (s *Set) rehash(newCap int) {
	oldKeys := s.keys
	oldGens := s.generations
	oldGen := s.gen

	s.keys = make([]uint32, newCap)
	s.generations = make([]uint32, newCap)
	s.gen = 1 // reset generation for new table
	s.size = 0

	mask := uint32(newCap - 1)
	for idx, g := range oldGens {
		if g != oldGen {
			continue // slot from previous generation – ignore
		}
		k := oldKeys[idx]
		j := k & mask
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

// nextPow2 returns the next power of two >= v.
func nextPow2(v uint32) uint32 {
	if v == 0 {
		return 1
	}
	if bits.OnesCount32(v) == 1 {
		return v // already power of two
	}
	return 1 << (32 - bits.LeadingZeros32(v))
}
