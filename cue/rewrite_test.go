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
	"fmt"
	"strings"
)

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

var indentLevel int

func (c *context) ind() int {
	old := indentLevel
	indentLevel += 2
	return old
}

func (c *context) unindent(old int) {
	indentLevel = old
}

func (c *context) printIndent(args ...interface{}) {
	fmt.Print(strings.Repeat("  ", indentLevel))
	c.println(args...)
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
		x = x.expandFields(ctx)
		arcs := make(arcs, len(x.arcs))
		for i, a := range x.arcs {
			v := x.at(ctx, i)
			arcs[i] = arc{a.feature, rewriteRec(ctx, a.v, v, m), nil, a.attrs}
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
		obj := &structLit{x.baseValue, emit, t, nil, arcs, nil}
		return obj
	case *list:
		a := make([]value, len(x.a))
		for i := range x.a {
			a[i] = rewriteRec(ctx, x.a[i], x.at(ctx, i), m)
		}
		len := rewriteRec(ctx, x.len, x.len.(evaluated), m)
		typ := rewriteRec(ctx, x.typ, x.typ.(evaluated), m)
		return &list{x.baseValue, a, typ, len}
	default:
		return eval
	}
}
