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

// WeakMap is a thread-safe map that holds weak references to values.
// Entries are automatically removed when values are garbage collected.
// This can be reused for string interning and other caching needs.
type WeakMap[K comparable, V any] struct {
	m sync.Map
}

type entry[K, V any] struct {
	k K
	p weak.Pointer[V]
	c runtime.Cleanup
}

// Store adds or replaces a value in the map with a weak reference.
// The entry will be automatically removed when the value is garbage collected.
func (m *WeakMap[K, V]) Store(k K, p *V) {
	e := &entry[K, V]{
		k: k,
		p: weak.Make(p),
	}
	e.c = runtime.AddCleanup(p, func(e *entry[K, V]) {
		m.m.CompareAndDelete(e.k, e)
	}, e)
	old, ok := m.m.Swap(k, e)
	if ok {
		old.(*entry[K, V]).c.Stop()
	}
}

// Load retrieves a value from the map. Returns nil if the key is not found
// or if the value has been garbage collected.
func (m *WeakMap[K, V]) Load(k K) *V {
	v, ok := m.m.Load(k)
	if !ok {
		return nil
	}
	return v.(*entry[K, V]).p.Value()
}
