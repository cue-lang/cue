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
		if len(x.Conjuncts) == 0 || x.IsData() {
			// Treat as literal value.
			return e.value(x)
		} // Should this be the arcs label?

		a := []conjunct{}
		for _, c := range x.Conjuncts {
			a = append(a, conjunct{c, 0})
		}

		return e.mergeValues(adt.InvalidLabel, x, a, x.Conjuncts...)

	case *adt.StructLit:
		c := adt.MakeRootConjunct(nil, x)
		return e.mergeValues(adt.InvalidLabel, nil, []conjunct{{c: c, up: 0}}, c)

	case adt.Value:
		return e.value(x) // Use conjuncts.

	default:
		return e.adt(v, nil)
	}
}

// Piece out values:

// For a struct, piece out conjuncts that are already values. Those can be
// unified. All other conjuncts are added verbatim.

func (x *exporter) mergeValues(label adt.Feature, src *adt.Vertex, a []conjunct, orig ...adt.Conjunct) ast.Expr {

	e := conjuncts{
		exporter: x,
		values:   &adt.Vertex{},
		fields:   map[adt.Feature]field{},
	}

	_, saved := e.pushFrame(orig)
	defer e.popFrame(saved)

	for _, c := range a {
		e.top().upCount = c.up
		x := c.c.Expr()
		e.addExpr(c.c.Env, x)
	}

	s := x.top().scope

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

	// Sort fields in case features lists are missing to ensure
	// predictability. Also sort in reverse order, so that bugs
	// are more likely exposed.
	sort.Slice(fields, func(i, j int) bool {
		return fields[i] > fields[j]
	})

	m := sortArcs(extractFeatures(e.structs))
	sort.SliceStable(fields, func(i, j int) bool {
		if m[fields[j]] == 0 {
			return m[fields[i]] != 0
		}
		return m[fields[i]] > m[fields[j]]
	})

	if len(e.fields) == 0 && !e.hasEllipsis {
		switch len(e.exprs) {
		case 0:
			if len(e.structs) > 0 {
				return ast.NewStruct()
			}
			return ast.NewIdent("_")
		case 1:
			return e.exprs[0]
		case 2:
			// Simplify.
			return ast.NewBinExpr(token.AND, e.exprs...)
		}
	}

	for _, x := range e.exprs {
		s.Elts = append(s.Elts, &ast.EmbedDecl{Expr: x})
	}

	for _, f := range fields {
		field := e.fields[f]
		c := field.conjuncts

		label := e.stringLabel(f)

		if f.IsDef() {
			x.inDefinition++
		}

		a := []adt.Conjunct{}
		for _, cc := range c {
			a = append(a, cc.c)
		}

		merged := e.mergeValues(f, nil, c, a...)

		if f.IsDef() {
			x.inDefinition--
		}

		d := &ast.Field{Label: label, Value: merged}
		if isOptional(a) {
			d.Optional = token.Blank.Pos()
		}
		if x.cfg.ShowDocs {
			docs := extractDocs(src, a)
			ast.SetComments(d, docs)
		}
		if x.cfg.ShowAttributes {
			d.Attrs = ExtractFieldAttrs(a)
		}
		s.Elts = append(s.Elts, d)
	}
	if e.hasEllipsis {
		s.Elts = append(s.Elts, &ast.Ellipsis{})
	} else if src != nil && src.IsClosed(e.ctx) && e.inDefinition == 0 {
		return ast.NewCall(ast.NewIdent("close"), s)
	}

	return s
}

// Conjuncts if for collecting values of a single vertex.
type conjuncts struct {
	*exporter
	// Values is used to collect non-struct values.
	values      *adt.Vertex
	exprs       []ast.Expr
	structs     []*adt.StructLit
	fields      map[adt.Feature]field
	hasEllipsis bool
}

func (c *conjuncts) addConjunct(f adt.Feature, env *adt.Environment, n adt.Node) {

	x := c.fields[f]
	v := adt.MakeRootConjunct(env, n)
	x.conjuncts = append(x.conjuncts, conjunct{
		c:  v,
		up: c.top().upCount,
	})
	// x.upCounts = append(x.upCounts, c.top().upCount)
	c.fields[f] = x
}

type field struct {
	docs      []*ast.CommentGroup
	conjuncts []conjunct
}

type conjunct struct {
	c  adt.Conjunct
	up int32
}

func (e *conjuncts) addExpr(env *adt.Environment, x adt.Expr) {
	switch x := x.(type) {
	case *adt.StructLit:
		e.top().upCount++

		// Only add if it only has no bulk fields or elipsis.
		if isComplexStruct(x) {
			_, saved := e.pushFrame(nil)
			e.exprs = append(e.exprs, e.adt(x, nil))
			e.popFrame(saved)
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
				// TODO: mark optional here.
				label = f.Label
			case *adt.Ellipsis:
				e.hasEllipsis = true
				continue
			case adt.Expr:
				e.addExpr(env, f)
				continue

				// TODO: also handle dynamic fields
			default:
				panic("unreachable")
			}
			e.addConjunct(label, env, d)
		}
		e.top().upCount--

	case adt.Value: // other values.
		if v, ok := x.(*adt.Vertex); ok {
			if v.IsList() {
				a := []ast.Expr{}
				for _, x := range v.Elems() {
					a = append(a, e.expr(x))
				}
				if !v.IsClosed(e.ctx) {
					v := &adt.Vertex{}
					v.MatchAndInsert(e.ctx, v)
					a = append(a, &ast.Ellipsis{Type: e.expr(v)})
				}
				e.exprs = append(e.exprs, ast.NewList(a...))
				return
			}

			e.structs = append(e.structs, v.Structs...)

			// generated, only consider arcs.
			for _, a := range v.Arcs {
				a.Finalize(e.ctx) // TODO: should we do this?

				e.addConjunct(a.Label, env, a)
			}
			x = v.Value
			// e.exprs = append(e.exprs, e.value(v, v.Conjuncts...))
			// return
		}

		switch x.(type) {
		case *adt.StructMarker, *adt.Top:
		default:
			e.values.AddConjunct(adt.MakeRootConjunct(env, x)) // GOBBLE TOP
		}

	case *adt.BinaryExpr:
		switch {
		case x.Op == adt.AndOp:
			e.addExpr(env, x.X)
			e.addExpr(env, x.Y)
		case isSelfContained(x):
			e.values.AddConjunct(adt.MakeRootConjunct(env, x))
		default:
			e.exprs = append(e.exprs, e.expr(x))
		}

	default:
		if isSelfContained(x) {
			e.values.AddConjunct(adt.MakeRootConjunct(env, x))
		} else {
			e.exprs = append(e.exprs, e.expr(x))
		}
	}
}

// TODO: find a better way to annotate optionality. Maybe a special conjunct
// or store it in the field information?
func isOptional(a []adt.Conjunct) bool {
	if len(a) == 0 {
		return false
	}
	for _, c := range a {
		switch f := c.Source().(type) {
		case nil:
			return false
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
