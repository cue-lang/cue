// Copyright 2021 CUE Authors
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
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
)

// This file contains query-related code.

// getScopePrefix finds the Vertex that exists in v for the longest prefix of p.
//
// It is used to make the parent scopes visible when resolving expressions.
func getScopePrefix(v Value, p Path) *adt.Vertex {
	for _, sel := range p.Selectors() {
		w := v.LookupPath(MakePath(sel))
		if !w.Exists() {
			break
		}
		v = w
	}
	return v.v
}

func errFn(pos token.Pos, msg string, args ...interface{}) {}

// resolveExpr binds unresolved expressions to values in the expression or v.
func resolveExpr(ctx *adt.OpContext, v *adt.Vertex, x ast.Expr) adt.Value {
	cfg := &compile.Config{Scope: v}

	astutil.ResolveExpr(x, errFn)

	c, err := compile.Expr(cfg, ctx, pkgID(), x)
	if err != nil {
		return &adt.Bottom{Err: err}
	}
	return adt.Resolve(ctx, c)
}

// LookupPath reports the value for path p relative to v.
func (v Value) LookupPath(p Path) Value {
	if v.v == nil {
		return Value{}
	}
	n := v.v
	ctx := v.ctx()

outer:
	for _, sel := range p.path {
		f := sel.sel.feature(v.idx)
		for _, a := range n.Arcs {
			if a.Label == f {
				n = a
				continue outer
			}
		}
		if sel.sel.optional() {
			x := &adt.Vertex{
				Parent: v.v,
				Label:  sel.sel.feature(ctx),
			}
			n.MatchAndInsert(ctx, x)
			if len(x.Conjuncts) > 0 {
				x.Finalize(ctx)
				n = x
				continue
			}
		}

		var x *adt.Bottom
		if err, ok := sel.sel.(pathError); ok {
			x = &adt.Bottom{Err: err.Error}
		} else {
			// TODO: better message.
			x = mkErr(v.idx, n, adt.NotExistError, "field %q not found", sel.sel)
		}
		v := makeValue(v.idx, n)
		return newErrValue(v, x)
	}
	return makeValue(v.idx, n)
}
