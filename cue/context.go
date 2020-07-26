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
	"github.com/cockroachdb/apd/v2"
)

// context manages evaluation state.
type context struct {
	opCtx *adt.OpContext
	*apd.Context
	*index
}

var baseContext apd.Context

func init() {
	baseContext = apd.BaseContext
	baseContext.Precision = 24
}

// newContext returns a new evaluation context.
func (idx *index) newContext() *context {
	c := &context{
		Context: &baseContext,
		index:   idx,
	}
	if idx != nil {
		c.opCtx = eval.NewContext(idx.Runtime, nil)
	}
	return c
}

func debugStr(ctx *context, v adt.Node) string {
	return debug.NodeString(ctx.opCtx, v, nil)
}

func (c *context) str(v adt.Node) string {
	return debugStr(c, v)
}

func (c *context) mkErr(src source, args ...interface{}) *bottom {
	return c.index.mkErr(src, args...)
}

func (c *context) vertex(v *adt.Vertex) *adt.Vertex {
	return v
}

// vertex returns the evaluated vertex of v.
func (v Value) vertex(ctx *context) *adt.Vertex {
	return ctx.vertex(v.v)
}

// eval returns the evaluated value. This may not be the vertex.
//
// Deprecated: use ctx.value
func (v Value) eval(ctx *context) adt.Value {
	if v.v == nil {
		panic("undefined value")
	}
	x := ctx.manifest(v.v)
	switch x.Kind() {
	case adt.StructKind, adt.ListKind:
		return x
	default:
		return x.Value
	}
}

// func (v Value) evalFull(u value) (Value, adt.Value) {
// 	ctx := v.ctx()
// 	x := ctx.manifest(u)
// }

// TODO: change from Vertex to Vertex.
func (c *context) manifest(v *adt.Vertex) *adt.Vertex {
	v.Finalize(c.opCtx)
	return v
}
