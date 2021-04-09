// Copyright 2018 The CUE Authors
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
	"cuelang.org/go/internal/core/debug"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/core/runtime"
)

// newContext returns a new evaluation context.
func newContext(idx *runtime.Runtime) *adt.OpContext {
	if idx == nil {
		return nil
	}
	return eval.NewContext(idx, nil)
}

func debugStr(ctx *adt.OpContext, v adt.Node) string {
	return debug.NodeString(ctx, v, nil)
}

func str(c *adt.OpContext, v adt.Node) string {
	return debugStr(c, v)
}

// eval returns the evaluated value. This may not be the vertex.
//
// Deprecated: use ctx.value
func (v Value) eval(ctx *adt.OpContext) adt.Value {
	if v.v == nil {
		panic("undefined value")
	}
	x := manifest(ctx, v.v)
	return x.Value()
}

// TODO: change from Vertex to Vertex.
func manifest(ctx *adt.OpContext, v *adt.Vertex) *adt.Vertex {
	v.Finalize(ctx)
	return v
}
