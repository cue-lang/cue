// Copyright 2026 CUE Authors
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

package style

import (
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

// inlineStructValue strips the Lbrace/Rbrace of f's value StructLit
// when chain-collapsing it would not lose any side data, per the
// [canInlineFieldValue] criteria. We iterate down the chain until a
// node no longer qualifies, so a deep nest of single-Field structs
// collapses in one call.
//
// When we strip braces, we also clear the inner Field's label RelPos if
// it carries Newline/NewSection: in chain form `outer: inner: leaf` the
// inner field sits on the outer's line, so the hint would otherwise
// push the chain back onto multiple lines.
//
// Returns true if any Lbrace/Rbrace was zeroed.
func inlineStructValue(f *ast.Field) bool {
	changed := false
	for {
		sl := canInlineFieldValue(f)
		if sl == nil {
			return changed
		}
		if sl.Lbrace.IsValid() || sl.Rbrace.IsValid() {
			sl.Lbrace = token.NoPos
			sl.Rbrace = token.NoPos
			changed = true
		}
		inner := sl.Elts[0].(*ast.Field)
		// canInlineFieldValue rejects any inner Field carrying
		// comments, so the label's own Pos is the only place a
		// Newline/NewSection hint can live.
		if inner.Label.Pos().RelPos() >= token.Newline {
			ast.SetPos(inner.Label, inner.Label.Pos().WithRel(token.NoRelPos))
			changed = true
		}
		// Recurse into the freshly-unbraced inner Field.
		f = inner
	}
}

// canInlineFieldValue reports whether f's value can be unbraced for
// chain form, returning that StructLit or nil. The value must be a
// 1-element StructLit whose single element is a Field, and none of the
// outer Field, the inner Field, or the StructLit may carry side data
// that the chain shape would displace or lose.
//
// We also refuse when f owns a doc comment ([ast.DocComments]
// non-empty): f's value is a braced struct, so f is currently the
// leaf of its field-chain and thus the semantic owner of any doc on
// the chain.  Unbracing it would extend the chain past f and move
// that ownership to the inner field, silently re-targeting the
// comment.
func canInlineFieldValue(f *ast.Field) *ast.StructLit {
	if len(f.Attrs) > 0 {
		return nil
	}
	sl, ok := f.Value.(*ast.StructLit)
	if !ok || len(sl.Elts) != 1 || len(ast.Comments(sl)) > 0 {
		return nil
	}
	inner, ok := sl.Elts[0].(*ast.Field)
	if !ok {
		return nil
	}
	if len(inner.Attrs) > 0 || len(ast.Comments(inner)) > 0 {
		return nil
	}
	if len(ast.DocComments(f)) > 0 {
		return nil
	}
	return sl
}
