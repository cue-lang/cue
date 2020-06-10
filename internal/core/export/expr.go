// Copyright 2020 CUE Authors
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

package export

import (
	"sort"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/adt"
)

// Modes:
//   raw: as is
//   def: merge structs, print reset as is.
//
// Possible simplifications in def mode:
//    - merge contents of multiple _literal_ structs.
//      - this is not possible if some of the elements are bulk optional
//        (or is it?).
//    - still do not ever resolve references.
//    - to do this, fields must be pre-linked to their destinations.
//    - use astutil.Sanitize to resolve shadowing and imports.
//
//
// Categories of printing:
//   - concrete
//   - optionals
//   - references
//   - constraints
//
// Mixed mode is also not supported in the old implementation (at least not
// correctly). It requires references to resolve properly, backtracking to
// a common root and prefixing that to the reference. This is now possible
// with the Environment construct and could be done later.

func (e *exporter) expr(v adt.Expr) (result ast.Expr) {
	switch x := v.(type) {
	case nil:
		return nil

	case *adt.Vertex:
		if len(x.Conjuncts) == 0 {
			// Treat as literal value.
			return e.value(x)
		}
		return e.mergeValues(x.Conjuncts...)

	case *adt.StructLit:
		return e.mergeValues(adt.MakeConjunct(nil, x))

	case adt.Value:
		e.value(x)

	default:
		if f, ok := x.Source().(*ast.File); ok {
			return &ast.StructLit{Elts: f.Decls}
		}

		return v.Source().(ast.Expr)
	}
	return nil
}

// Piece out values:

// For a struct, piece out conjuncts that are already values. Those can be
// unified. All other conjuncts are added verbatim.

func (x *exporter) mergeValues(a ...adt.Conjunct) ast.Expr {
	e := conjuncts{
		exporter: x,
		values:   &adt.Vertex{},
		fields:   map[adt.Feature][]adt.Conjunct{},
	}

	for _, c := range a {
		e.addExpr(c.Env, c.Expr())
	}

	// Unify values only for one level.
	if len(e.values.Conjuncts) > 0 {
		e.values.Finalize(e.ctx)
		e.exprs = append(e.exprs, e.value(e.values, e.values.Conjuncts...))
	}

	// Collect and order set of fields.
	fields := []adt.Feature{}
	for f := range e.fields {
		fields = append(fields, f)
	}
	m := sortArcs(e.exporter.extractFeatures(e.structs))
	sort.SliceStable(fields, func(i, j int) bool {
		if m[fields[i]] == 0 {
			return m[fields[j]] != 0
		}
		return m[fields[i]] > m[fields[j]]
	})

	if len(e.fields) == 0 && !e.hasEllipsis {
		switch len(e.exprs) {
		case 0:
			return ast.NewIdent("_")
		case 1:
			return e.exprs[0]
		case 2:
			// Simplify.
			return ast.NewBinExpr(token.AND, e.exprs...)
		}
	}

	s := &ast.StructLit{}
	for _, x := range e.exprs {
		s.Elts = append(s.Elts, &ast.EmbedDecl{Expr: x})
	}

	for _, f := range fields {
		c := e.fields[f]
		merged := e.mergeValues(c...)
		label := e.stringLabel(f)
		d := &ast.Field{Label: label, Value: merged}
		if isOptional(c) {
			d.Optional = token.Blank.Pos()
		}
		s.Elts = append(s.Elts, d)
	}
	if e.hasEllipsis {
		s.Elts = append(s.Elts, &ast.Ellipsis{})
	}

	return s
}

// A conjuncts collects values of a single vertex.
type conjuncts struct {
	*exporter
	// Values is used to collect non-struct values.
	values      *adt.Vertex
	exprs       []ast.Expr
	structs     []*adt.StructLit
	fields      map[adt.Feature][]adt.Conjunct
	hasEllipsis bool
}

func (e *conjuncts) addExpr(env *adt.Environment, x adt.Expr) {
	switch x := x.(type) {
	case *adt.StructLit:
		// Only add if it only has no bulk fields or elipsis.
		if isComplexStruct(x) {
			switch src := x.Src.(type) {
			case nil:
				panic("now allowed")
			case *ast.StructLit:
				e.exprs = append(e.exprs, src)
			case *ast.File:
				e.exprs = append(e.exprs, &ast.StructLit{Elts: src.Decls})
			}
			return
		}
		// Used for sorting.
		e.structs = append(e.structs, x)

		for _, d := range x.Decls {
			var label adt.Feature
			switch f := d.(type) {
			case *adt.Field:
				label = f.Label
			case *adt.OptionalField:
				label = f.Label
			case *adt.Ellipsis:
				e.hasEllipsis = true
			case adt.Expr:
				e.addExpr(env, f)
				continue

				// TODO: also handle dynamic fields
			default:
				panic("unreachable")
			}
			c := adt.MakeConjunct(env, d)
			e.fields[label] = append(e.fields[label], c)
		}

	case adt.Value: // other values.
		if v, ok := x.(*adt.Vertex); ok {
			// if !v.IsList() {
			// 	panic("what to do?")
			// }
			// generated, only consider arcs.
			e.exprs = append(e.exprs, e.value(v, v.Conjuncts...))
			return
		}

		e.values.AddConjunct(adt.MakeConjunct(env, x))

	case *adt.BinaryExpr:
		switch {
		case x.Op == adt.AndOp:
			e.addExpr(env, x.X)
			e.addExpr(env, x.Y)
		case isSelfContained(x):
			e.values.AddConjunct(adt.MakeConjunct(env, x))
		default:
			e.exprs = append(e.exprs, e.expr(x))
		}

	default:
		if isSelfContained(x) {
			e.values.AddConjunct(adt.MakeConjunct(env, x))
		} else {
			e.exprs = append(e.exprs, e.expr(x))
		}
	}
}

func isOptional(a []adt.Conjunct) bool {
	for _, c := range a {
		switch f := c.Source().(type) {
		case *ast.Field:
			if f.Optional == token.NoPos {
				return false
			}
		}
	}
	return true
}

func isComplexStruct(s *adt.StructLit) bool {
	for _, e := range s.Decls {
		switch x := e.(type) {
		case *adt.Field, *adt.OptionalField, adt.Expr:

		case *adt.Ellipsis:
			if x.Value != nil {
				return true
			}

		default:
			return true
		}
	}
	return false
}

func isSelfContained(expr adt.Expr) bool {
	switch x := expr.(type) {
	case *adt.BinaryExpr:
		return isSelfContained(x.X) && isSelfContained(x.Y)
	case *adt.UnaryExpr:
		return isSelfContained(x.X)
	case *adt.BoundExpr:
		return isSelfContained(x.Expr)
	case adt.Value:
		return true
	}
	return false
}
