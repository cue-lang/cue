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
	"fmt"
	"strings"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/adt"
)

// This file contains query-related code.

// getScopePrefix finds the Vertex that exists in v for the longest prefix of p.
//
// It is used to make the parent scopes visible when resolving expressions.
func getScopePrefix(v Value, p Path) Value {
	for _, sel := range p.Selectors() {
		w := v.LookupPath(MakePath(sel))
		if !w.Exists() {
			break
		}
		v = w
	}
	return v
}

// LookupPath reports the value for path p relative to v.
//
// Use [AnyString] and [AnyIndex] to find the value of undefined element types
// for structs and lists respectively, for example for the patterns in
// `{[string]: int}` and `[...string]`.
//
// LookupPath ignores errors in the values it steps through: given
// `a: {x: 1, y: 1 & 2}`, looking up "a.x" yields 1 even though a is an
// error because of y. This differs from evaluating the selector a.x
// as a CUE expression, which results in the error of a.
func (v Value) LookupPath(p Path) Value {
	// TODO(v1): report errors from the values we step through,
	// consistent with the evaluation of selectors.
	if v.v == nil {
		return Value{}
	}
	n := v.v
	parent := v.parent_
	ctx := v.ctx()

outer:
	for _, sel := range p.path {
		if _, ok := sel.sel.(patternSelector); ok {
			// It's not possible to look up pattern constraints.
			// TODO: could potentially relax that restriction.
			err := errors.Newf(
				token.NoPos,
				"cannot look up pattern constraints other than AnyString or AnyIndex",
			)
			return newErrValue(makeValue(v.idx, n, parent), &adt.Bottom{Err: err})
		}
		f := sel.sel.feature(v.idx)
		deref := n.DerefValue()
		for _, a := range deref.Arcs {
			if a.Label == f {
				if a.IsConstraint() && !sel.sel.isConstraint() {
					break
				}
				a.Finalize(ctx)
				parent = linkParent(parent, n, a)
				n = a
				continue outer
			}
		}
		if sel.sel.isConstraint() {
			x := &adt.Vertex{
				Parent: n,
				Label:  sel.sel.feature(ctx),
			}
			deref.MatchAndInsert(ctx, x)
			if x.HasConjuncts() {
				x.Finalize(ctx)
				parent = linkParent(parent, n, x)
				n = x
				continue
			}
		}

		var x *adt.Bottom
		if err, ok := sel.sel.(pathError); ok {
			x = &adt.Bottom{Err: err.Error}
		} else {
			x = mkErr(n, adt.EvalError, "field not found: %v%s",
				sel.sel, hiddenScopeHint(ctx, deref, sel.sel))
			if n.Accept(ctx, f) {
				x.Code = adt.IncompleteError
			}
			x.NotExists = true
		}
		v := makeValue(v.idx, n, parent)
		return newErrValue(v, x)
	}
	return makeValue(v.idx, n, parent)
}

// hiddenScopeHint reports the package scopes under which v does hold a hidden
// field named like sel, for a hidden field selector which did not match.
// Hidden fields are scoped by package, see [Hid], and a value does not show
// those scopes anywhere, so a mismatched scope is easily mistaken for a
// missing field.
func hiddenScopeHint(ctx *adt.OpContext, v *adt.Vertex, sel selector) string {
	s, ok := sel.(scopedSelector)
	if !ok {
		return ""
	}
	var pkgs []string
	for _, a := range v.Arcs {
		if f := a.Label; f.IsHidden() && f.IdentString(ctx) == s.name {
			if pkg := f.PkgID(ctx); pkg != s.pkg {
				pkgs = append(pkgs, fmt.Sprintf("package %q", pkg))
			}
		}
	}
	if len(pkgs) == 0 {
		return ""
	}
	return fmt.Sprintf(" in package %q; hidden fields are scoped by package, and this value has %s in %s",
		s.pkg, s.name, strings.Join(pkgs, " and "))
}
