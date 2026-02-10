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

package adt_test

import (
	"sync"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

// TestDefaultConcurrent tests that calling Default() concurrently on a shared
// value with a disjunction is race-free. This is a regression test for
// https://github.com/cue-lang/cue/issues/2733
//
// The race occurred when multiple goroutines called Default() on a vertex with
// a single default (NumDefaults == 1), causing concurrent modification of the
// Conjuncts slice.
func TestDefaultConcurrent(t *testing.T) {
	ctx := cuecontext.New()

	// Create a value with a disjunction that has exactly 1 default.
	// This triggers the NumDefaults == 1 case in Vertex.Default().
	v := ctx.CompileString(`a: *"default" | "other"`)
	if err := v.Err(); err != nil {
		t.Fatal(err)
	}

	a := v.LookupPath(cue.ParsePath("a"))
	if err := a.Err(); err != nil {
		t.Fatal(err)
	}

	// Call Default() concurrently from multiple goroutines.
	// Without the fix, this would cause a data race on the Conjuncts slice.
	var wg sync.WaitGroup
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				d, ok := a.Default()
				if !ok {
					t.Error("expected default to exist")
					return
				}
				// Verify the default value is correct
				s, err := d.String()
				if err != nil {
					t.Error(err)
					return
				}
				if s != "default" {
					t.Errorf("expected 'default', got %q", s)
					return
				}
			}
		}()
	}
	wg.Wait()
}
