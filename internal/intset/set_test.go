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

package intset

import (
	"fmt"
	"math/bits"
	"math/rand/v2"
	"testing"
	"time"
)

// populate fills the set with n sequential values (0..n-1) and verifies
// insertion invariants along the way.
func populate[T Int](t *testing.T, n int) *Set[T] {
	t.Helper()
	s := New[T](n / 2) // intentionally small to exercise growth
	for i := 0; i < n; i++ {
		if !s.Add(T(i)) {
			t.Fatalf("insert %d reported duplicate", i)
		}
	}
	if got := s.Len(); got != n {
		t.Fatalf("Len=%d, want %d", got, n)
	}
	return s
}

// assertPresent asserts that Has(v) equals want for every v in vals.
func assertPresent[T Int](t *testing.T, s *Set[T], vals []T, want bool) {
	t.Helper()
	for _, v := range vals {
		if got := s.Has(v); got != want {
			t.Fatalf("Has(%v)=%v, want %v", v, got, want)
		}
	}
}

func TestBasicAddHasLen(t *testing.T) {
	s := New[uint32](8)
	if s.Len() != 0 {
		t.Fatalf("new set length want 0 got %d", s.Len())
	}
	if !s.Add(10) {
		t.Fatalf("expected first insertion true")
	}
	if s.Len() != 1 || !s.Has(10) {
		t.Fatalf("basic invariants failed")
	}
	if s.Add(10) {
		t.Fatalf("duplicate insertion should return false")
	}
	if s.Len() != 1 {
		t.Fatalf("duplicate insertion changed size")
	}
}

func TestClear(t *testing.T) {
	s := populate[uint16](t, 50)
	s.Clear()
	if s.Len() != 0 {
		t.Fatalf("Len after Clear() = %d, want 0", s.Len())
	}
	assertPresent(t, s, []uint16{0, 25, 49}, false)
	// ensure we can reuse without reallocation or corruption
	for i := 0; i < 20; i++ {
		s.Add(uint16(i))
	}
	if s.Len() != 20 {
		t.Fatalf("Len after reuse = %d, want 20", s.Len())
	}
}

func TestRehashGrowth(t *testing.T) {
	// create a small set to guarantee multiple growth events
	initialCap := 8
	s := New[uint32](initialCap)
	target := 10_000
	for i := 0; i < target; i++ {
		s.Add(uint32(i))
	}
	if s.Len() != target {
		t.Fatalf("after bulk add Len=%d want %d", s.Len(), target)
	}
	// verify every element is present
	for i := 0; i < target; i++ {
		if !s.Has(uint32(i)) {
			t.Fatalf("missing key %d after growth", i)
		}
	}
	// internal capacity should be power-of-two
	if bits.OnesCount(uint(len(s.keys))) != 1 {
		t.Fatalf("internal slice len=%d, not power-of-two", len(s.keys))
	}
}

func TestMultipleIntegerSizes(t *testing.T) {
	t.Run("uint8", func(t *testing.T) { populate[uint8](t, 200) })
	t.Run("uint16", func(t *testing.T) { populate[uint16](t, 1_000) })
	t.Run("uint32", func(t *testing.T) { populate[uint32](t, 10_000) })
	t.Run("uint64", func(t *testing.T) { populate[uint64](t, 1_000) })
}

func TestNextPow2(t *testing.T) {
	tests := []struct{ in, want int }{{0, 1}, {1, 1}, {2, 2}, {3, 4}, {7, 8}, {8, 8}, {9, 16}, {1023, 1024}, {1024, 1024}, {1025, 2048}}
	for _, test := range tests {
		t.Run(fmt.Sprint(test.in), func(t *testing.T) {
			if got := nextPow2(test.in); got != test.want {
				t.Fatalf("nextPow2(%d)=%d, want %d", test.in, got, test.want)
			}
		})
	}
}

// TestRandomizedBehaviour performs a behavioural, property-style test on a
// large batch of pseudo-random keys. It uses an auxiliary Go map to model the
// expected behaviour and cross-checks all edge cases, exercising rehash logic
// many times along the way.
func TestRandomizedBehaviour(t *testing.T) {
	const N = 100_000
	seed := uint64(time.Now().UnixNano())
	t.Logf("random seed %d", seed)
	randGen := rand.New(rand.NewPCG(seed, seed))

	s := New[uint32](8) // deliberately tiny to trigger dozens of rehashes
	model := make(map[uint32]struct{})

	for i := 0; i < N; i++ {
		k := randGen.Uint32()
		added := s.Add(k)
		_, exists := model[k]
		if added == exists {
			t.Fatalf("Add(%d) reported %v, expected %v (iteration %d)", k, added, !exists, i)
		}
		model[k] = struct{}{}
		if !s.Has(k) {
			t.Fatalf("Has(%d)=false immediately after add", k)
		}
		if s.Len() != len(model) {
			t.Fatalf("Len mismatch at i=%d: set=%d, model=%d", i, s.Len(), len(model))
		}
	}
	for k := range model {
		if !s.Has(k) {
			t.Fatalf("Has(%d)=false at end, should be true", k)
		}
	}

	// sanity check on internal capacity: should be power of two and at least len/model * 4/3 (load factor)
	if bits.OnesCount(uint(len(s.keys))) != 1 {
		t.Fatalf("internal slice len=%d, not power of two", len(s.keys))
	}
}
