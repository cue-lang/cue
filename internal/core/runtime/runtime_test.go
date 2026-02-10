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

package runtime

import (
	"sync"
	"testing"

	"cuelang.org/go/cue/build"
)

// TestBuildDataConcurrent tests that concurrent access to BuildData and
// SetBuildData is race-free. This is a regression test for
// https://github.com/cue-lang/cue/issues/2733
//
// The race occurred when one goroutine called SetBuildData (map write) while
// another called BuildData (map read) on the same runtime's loaded map.
func TestBuildDataConcurrent(t *testing.T) {
	r := New()

	// Create multiple build instances to operate on.
	instances := make([]*build.Instance, 10)
	for i := range instances {
		instances[i] = &build.Instance{}
	}

	// Concurrently read and write to the loaded map.
	// Without the fix, this would cause a fatal error:
	// "concurrent map read and map write"
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(2)
		// Writer goroutine
		go func() {
			defer wg.Done()
			for i, inst := range instances {
				r.SetBuildData(inst, i)
			}
		}()
		// Reader goroutine
		go func() {
			defer wg.Done()
			for _, inst := range instances {
				r.BuildData(inst)
			}
		}()
	}
	wg.Wait()

	// Verify data integrity after concurrent access.
	for i, inst := range instances {
		if v, ok := r.BuildData(inst); ok {
			if v != i {
				t.Errorf("BuildData(%d) = %v, want %d", i, v, i)
			}
		}
	}
}
