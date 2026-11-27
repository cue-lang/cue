// Copyright 2026 CUE Authors
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
	"reflect"
	"testing"
	"unsafe"
)

// TestSizeof checks the sizes of types which evaluation allocates or copies
// in large numbers. Growing one past its size class costs the next class up
// on every allocation, which is very likely to increase memory usage for
// users with large configurations, so consider any such increase carefully.
func TestSizeof(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("sizes are only checked on 64-bit platforms")
	}
	// Go's size classes around the types allocated one at a time, the same in
	// Go 1.26, 1.27 and 1.28-devel:
	//
	//	16, 24, 32, ..., 160, 176, 192, ..., 768, 896, 1024
	tests := []struct {
		val  any
		size uintptr
	}{
		{nodeContext{}, 896}, // allocated one at a time
		{Vertex{}, 176},      // allocated one at a time
		{task{}, 128},        // allocated in chunks
		{Environment{}, 24},  // allocated one at a time
		{Conjunct{}, 72},     // held in slices
		{CloseInfo{}, 48},    // embedded in conjuncts and tasks
	}
	for _, test := range tests {
		typ := reflect.TypeOf(test.val)
		if got := typ.Size(); got != test.size {
			t.Errorf("unsafe.Sizeof(%v) = %d, want %d", typ, got, test.size)
		}
	}
}
