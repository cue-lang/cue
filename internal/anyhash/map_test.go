// Copyright 2025 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package anyhash_test

import (
	"hash/maphash"
	"slices"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"cuelang.org/go/internal/anyhash"
	"cuelang.org/go/internal/anyunique"
)

// caseInsensitive is a string Hasher that ignores letter case.
type caseInsensitive struct{}

var _ anyunique.Hasher[string] = caseInsensitive{}

func (caseInsensitive) Hash(h *maphash.Hash, s string) {
	var buf [utf8.UTFMax]byte
	for _, r := range s {
		n := utf8.EncodeRune(buf[:], unicode.ToLower(r))
		h.Write(buf[:n])
	}
}

func (caseInsensitive) Equal(x, y string) bool {
	return strings.EqualFold(x, y)
}

func TestMap(t *testing.T) {
	m := anyhash.NewMap[string, int](caseInsensitive{})

	// Length of empty map.
	if l := m.Len(); l != 0 {
		t.Errorf("Len() on empty Map: got %d, want 0", l)
	}
	// At of missing key.
	if v := m.At("foo"); v != 0 {
		t.Errorf("At() on empty Map: got %v, want 0", v)
	}
	// Deletion of missing key.
	if _, removed := m.Delete("foo"); removed {
		t.Errorf("Delete() on empty Map: got true, want false")
	}
	// Contains of missing key.
	if m.Contains("foo") {
		t.Errorf("Contains() on empty Map: got true, want false")
	}

	// Set of new key.
	if prev, changed := m.Set("Hello", 1); prev != 0 {
		t.Errorf("Set() on empty Map returned non-zero previous value %d", prev)
	} else if !changed {
		t.Errorf("Set() on empty Map returned changed=false")
	}

	// Now: {"Hello": 1}

	if l := m.Len(); l != 1 {
		t.Errorf("Len(): got %d, want 1", l)
	}
	if v := m.At("Hello"); v != 1 {
		t.Errorf(`At("Hello"): got %v, want 1`, v)
	}
	// Case-insensitive get
	if v := m.At("hello"); v != 1 {
		t.Errorf(`At("hello"): got %v, want 1`, v)
	}
	if v := m.At("HELLO"); v != 1 {
		t.Errorf(`At("HELLO"): got %v, want 1`, v)
	}
	if !m.Contains("hElLo") {
		t.Errorf(`Contains("hElLo") returned false`)
	}

	// Update existing key
	if prev, changed := m.Set("HELLO", 2); prev != 1 {
		t.Errorf(`Set("HELLO") previous value: got %d, want 1`, prev)
	} else if changed {
		t.Errorf(`Set("HELLO") returned changed=true`)
	}

	// Set another key
	if prev, changed := m.Set("World", 3); prev != 0 {
		t.Errorf(`Set("World") previous value: got %d, want 0`, prev)
	} else if !changed {
		t.Errorf(`Set("World") returned changed=false`)
	}

	// Test iterators
	keys := slices.Collect(m.Keys())
	slices.Sort(keys)
	if !slices.Equal(keys, []string{"Hello", "World"}) {
		t.Errorf("Keys(): got %v", keys)
	}

	values := slices.Collect(m.Values())
	slices.Sort(values)
	if !slices.Equal(values, []int{2, 3}) {
		t.Errorf("Values(): got %v", values)
	}

	entries := make(map[string]int)
	for k, v := range m.All() {
		entries[k] = v
	}
	if len(entries) != 2 || entries["Hello"] != 2 || entries["World"] != 3 {
		t.Errorf("All(): got %v", entries)
	}

	// Clone
	m2 := m.Clone()
	if l := m2.Len(); l != 2 {
		t.Errorf("Clone.Len() = %d, want 2", l)
	}
	if m2.At("hello") != 2 {
		t.Errorf(`Clone.At("hello") = %d`, m2.At("hello"))
	}

	// Delete existing
	if prev, removed := m.Delete("hello"); !removed || prev != 2 {
		t.Errorf(`Delete("hello") = %v, %v, want 2, true`, prev, removed)
	}

	// Now: {"World": 3}

	if l := m.Len(); l != 1 {
		t.Errorf("Len(): got %d, want 1", l)
	}
	if m.Contains("Hello") {
		t.Errorf(`Contains("Hello") returned true after Delete`)
	}

	// Clear
	m.Clear()
	if l := m.Len(); l != 0 {
		t.Errorf("Len() after Clear: got %d, want 0", l)
	}
	if m.Contains("World") {
		t.Errorf(`Contains("World") returned true after Clear`)
	}

	// Ensure m2 (clone) is unaffected by Clear
	if l := m2.Len(); l != 2 {
		t.Errorf("m2.Len() after m.Clear() = %d, want 2", l)
	}
}

func TestNilMapPanics(t *testing.T) {
	panics := func(name string, f func()) {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("expected panic for nil Map.%s", name)
			}
		}()
		f()
	}

	var m *anyhash.Map[string, int]

	panics("Len", func() { m.Len() })
	panics("All", func() { m.All() })
	panics("Keys", func() { m.Keys() })
	panics("Values", func() { m.Values() })
	panics("Get", func() { m.Get("key") })
	panics("At", func() { m.At("key") })
	panics("Contains", func() { m.Contains("key") })
	panics("Set", func() { m.Set("key", 1) })
	panics("Delete", func() { m.Delete("key") })
	panics("Clear", func() { m.Clear() })
	panics("Clone", func() { m.Clone() })
	panics("InsertAll", func() { m.SetAll(func(func(string, int) bool) {}) })
}
