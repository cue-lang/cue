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

type rewriteMode int

const (
	evalRaw rewriteMode = iota
	evalSimplify
	evalPartial // all but disjunctions
	evalFull
)

// testResolve recursively
func testResolve(ctx *context, v value, m rewriteMode) (result value) {
	if m == evalRaw || v == nil {
		return v
	}
	return rewriteRec(ctx, v, v.evalPartial(ctx), m)
}

func rewriteRec(ctx *context, raw value, eval evaluated, m rewriteMode) (result value) {
	if m >= evalPartial {
		if isIncomplete(eval) {
			return raw
		}
		if m == evalFull {
			eval = ctx.manifest(eval)
			if isBottom(eval) {
				return eval
			}
		}
	}
	switch x := eval.(type) {
	case *structLit:
		if m == evalFull {
			e := ctx.manifest(x)
			if isBottom(e) {
				return e
			}
			x = e.(*structLit)
		}
		var err *bottom
		x, err = x.expandFields(ctx)
		if err != nil {
			if isIncomplete(err) {
				return raw
			}
			return err
		}
		arcs := make(arcs, len(x.arcs))
		for i, a := range x.arcs {
			v := x.at(ctx, i)
			a.setValue(rewriteRec(ctx, a.v, v, m))
			arcs[i] = a
		}
		t := x.template
		if t != nil {
			v := rewriteRec(ctx, t, t.evalPartial(ctx), m)
			if isBottom(v) {
				return v
			}
			t = v
		}
		emit := testResolve(ctx, x.emit, m)
		obj := &structLit{x.baseValue, emit, t, x.isClosed, nil, arcs, nil}
		return obj
	case *list:
		elm := rewriteRec(ctx, x.elem, x.elem, m).(*structLit)
		len := rewriteRec(ctx, x.len, x.len.(evaluated), m)
		typ := rewriteRec(ctx, x.typ, x.typ.evalPartial(ctx), m)
		return &list{x.baseValue, elm, typ, len}
	default:
		return eval
	}
}
