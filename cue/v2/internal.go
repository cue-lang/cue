// Copyright 2026 The CUE Authors
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

package cue

import (
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/v2bridge"
)

// Register the value constructor with the bridge package so that
// loaders can construct values without this package exporting internal
// constructors.
func init() {
	v2bridge.NewVertexValue = func(rt *runtime.Runtime, v *adt.Vertex) any {
		return newVertexValue(rt, v)
	}
	v2bridge.VertexOf = func(x any) (*runtime.Runtime, *adt.Vertex) {
		v, ok := x.(Value)
		if !ok || v.op == nil {
			return nil, nil
		}
		v.op.mu.Lock()
		defer v.op.mu.Unlock()
		if v.op.built == nil || v.op.memoRT != v.rt {
			// The value has not been realized (for this runtime).
			return v.rt, nil
		}
		return v.rt, v.op.built
	}
}
