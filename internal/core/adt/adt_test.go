// Copyright 2025 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package adt_test

import (
	"testing"

	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"
)

// TestEmptyNodeInert checks that initializing the shared empty node, as
// returned for a failed lookup or list conversion, does not create a
// nodeContext for it.
func TestEmptyNodeInert(t *testing.T) {
	ctx := adt.NewContext(runtime.New(), &adt.Vertex{})

	// RawList returns the shared empty node for a value which is not a list.
	empty := ctx.RawList(&adt.Top{})
	ctx.Err() // discard the type error
	if empty != ctx.RawList(&adt.Top{}) {
		t.Fatal("RawList did not return a shared node")
	}
	ctx.Err()

	// The empty node is shared, so an earlier test may already have given
	// it a nodeContext. Check that it is finalized, and separately that a
	// fresh finalized vertex does not get a nodeContext either.
	done := &adt.Vertex{}
	done.ForceDone()
	if empty.Status() != done.Status() {
		t.Fatalf("empty node has status %v, want %v", empty.Status(), done.Status())
	}
	for _, v := range []*adt.Vertex{empty, done} {
		before := *ctx.Stats()
		v.Init(ctx)
		after := *ctx.Stats()
		if after.Allocs != before.Allocs || after.Reused != before.Reused {
			t.Fatalf("finalized node got a nodeContext: allocs %d -> %d, reused %d -> %d",
				before.Allocs, after.Allocs, before.Reused, after.Reused)
		}
	}
}
