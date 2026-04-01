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

package cuetxtar

import (
	"fmt"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	cueerrors "cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/export"
	"cuelang.org/go/internal/value"
)

// cmpCtx carries comparison options through the recursive astCmp calls.
type cmpCtx struct {
	// baseLine is the 1-indexed source line used to resolve relative pos=
	// specs in nested @test(err) directives. When 0, deltaLine values in
	// pos specs are treated as absolute line numbers.
	baseLine int
}

// astCompare compares a parsed CUE AST expression against an evaluated
// cue.Value. It returns nil when they match, or a descriptive error.
//
// Unlike compiling the expected expression and using Value.Equal or diff.Diff,
// this approach avoids masking evaluator bugs — the expected value is never
// compiled, so a bug that affects both sides equally cannot hide a mismatch.
//
// Additionally, this enables richer comparison than cue.Final() allows:
//   - Disjunctions are compared structurally, order-independently, preserving
//     default markers.
//   - Pattern constraints ([T]: V) are checked for existence and value.
//   - Definitions (#D) and hidden definitions (_#D) are compared.
func astCompare(expr ast.Expr, val cue.Value) error {
	return (&cmpCtx{}).astCmp(nil, expr, val)
}

// astCmp is the recursive comparison workhorse. path accumulates the
// human-readable location for error messages.
//
// It checks structural consistency: if the value is a disjunction but the
// expected AST is not (or vice versa for conjunctions), an error is reported.
// Use @test(final) on a field in the expected struct to opt into
// default-resolution, allowing a plain value to match a disjunction default.
func (c *cmpCtx) astCmp(path []string, expr ast.Expr, val cue.Value) error {
	// Structural consistency check: if the value is a disjunction or
	// conjunction, the expected AST must reflect that structure.
	if err := checkStructuralMatch(path, expr, val); err != nil {
		return err
	}

	switch e := expr.(type) {
	case *ast.StructLit:
		return c.cmpStruct(path, e, val)
	case *ast.ListLit:
		return c.cmpList(path, e, val)
	case *ast.BinaryExpr:
		switch e.Op {
		case token.AND:
			return c.cmpConjunction(path, e, val)
		case token.OR:
			return c.cmpDisjunction(path, e, val)
		}
	case *ast.UnaryExpr:
		// evaluate non-boundary expressions.
		switch e.Op {
		case token.ADD, token.SUB, token.NOT:
			return cmpFinal(path, e, val)
		}
		return c.cmpUnaryExpr(path, e, val)
	case *ast.BasicLit:
		return cmpFinal(path, e, val)
	case *ast.Ident:
		return cmpIdent(path, e, val)
	case *ast.ParenExpr:
		return c.astCmp(path, e.X, val)
	case *ast.BottomLit:
		return nil
	}
	return pathErr(path, "unsupported AST node type %T", expr)
}

// cmpFinal compiles the AST expression and checks that the compiled value
// equals val. This is the @test(final) path: it opts out of structural
// AST comparison in favor of compile-and-compare. Defaults are resolved
// on val before the check.
func cmpFinal(path []string, expr ast.Expr, val cue.Value) error {
	b, err := format.Node(expr)
	if err != nil {
		return pathErr(path, "cannot format expression: %v", err)
	}
	ctx := val.Context()
	expected := ctx.CompileString(string(b))
	if err := expected.Err(); err != nil {
		return pathErr(path, "cannot compile expression %q: %v", b, err)
	}
	if !valuesEqual(expected, val) {
		return pathErr(path, "expected %s, got %v", b, val)
	}
	return nil
}

// valuesEqual compares two cue.Values for equality, ignoring ArcType
// differences. This is necessary because values from optional/required
// fields have a different ArcType than compiled values, which causes
// cue.Value.Equals to return false even when the values are identical.
func valuesEqual(a, b cue.Value) bool {
	if a.Equals(b) {
		return true
	}
	// Normalize ArcType: temporarily set b's ArcType to match a's.
	av, bv := a.Core().V, b.Core().V
	if av.ArcType == bv.ArcType {
		return false // ArcType already matches; genuine inequality.
	}
	saved := bv.ArcType
	bv.ArcType = av.ArcType
	opCtx := value.OpContext(a)
	eq := adt.Equal(opCtx, av, bv, 0)
	bv.ArcType = saved
	return eq
}

// isASTDisjunction reports whether expr represents a disjunction (| or *).
func isASTDisjunction(expr ast.Expr) bool {
	switch e := expr.(type) {
	case *ast.BinaryExpr:
		return e.Op == token.OR
	case *ast.ParenExpr:
		return isASTDisjunction(e.X)
	}
	return false
}

// isASTConjunction reports whether expr represents a conjunction (&).
func isASTConjunction(expr ast.Expr) bool {
	e, ok := expr.(*ast.BinaryExpr)
	return ok && e.Op == token.AND
}

// checkStructuralMatch checks that the AST and value agree on whether
// they are disjunctions or conjunctions. This prevents a plain expected
// value like 1 from silently matching *1 | 2 through default-resolution.
//
// The check is skipped for concrete values: a concrete int like 1 is fine
// even if its Expr() decomposes to a conjunction (e.g. from a pattern
// constraint int & 1), since the result is unambiguously concrete.
func checkStructuralMatch(path []string, expr ast.Expr, val cue.Value) error {
	if val.IsConcrete() {
		return nil
	}
	// Struct literals may contain @test(final) which handles mismatches
	// internally via cmpFinal, so defer the check to cmpStruct.
	if _, ok := expr.(*ast.StructLit); ok {
		return nil
	}
	tv := val.Core()
	switch tv.V.BaseValue.(type) {
	case *adt.Disjunction:
		if !isASTDisjunction(expr) {
			return pathErr(path, "value is a disjunction but expected expression is not; "+
				"use a disjunction in the expected value or @test(final) on the field")
		}
	case *adt.Conjunction:
		if !isASTConjunction(expr) {
			return pathErr(path, "value is a conjunction but expected expression is not; "+
				"use a conjunction in the expected value")
		}
	}

	return nil
}

// ── structs ─────────────────────────────────────────────────────────────────

func (c *cmpCtx) cmpStruct(path []string, s *ast.StructLit, val cue.Value) error {
	// Collect expected fields, let bindings, and pattern constraints from the AST.
	// Also detect @test(checkOrder), @test(final), and non-@test attributes.
	type expectedField struct {
		name       string
		isDef      bool
		isHidden   bool
		constraint token.Token // token.ILLEGAL (regular), token.OPTION (?), token.NOT (!)
		final      bool        // @test(final): resolve default before comparing
		ignore     bool        // @test(ignore): skip eq descent; field need not exist
		errCheck   *errArgs    // @test(err, ...): check value is error instead of comparing
		value      ast.Expr
		attrs      []*ast.Attribute // non-@test attributes
	}
	type expectedLet struct {
		name string
		expr ast.Expr
	}
	type expectedPattern struct {
		label ast.Expr // the expression inside [...]
		value ast.Expr
	}

	var fields []expectedField
	var lets []expectedLet
	var patterns []expectedPattern
	checkOrder := false
	allFinal := false

	for _, d := range s.Elts {
		switch d := d.(type) {
		case *ast.Attribute:
			// Look for @test directives; ignore other @test attributes.
			if k, _ := d.Split(); k == "test" {
				pa, err := parseTestAttr(d)
				if err == nil {
					switch pa.directive {
					case "checkOrder":
						checkOrder = true
					case "final":
						allFinal = true
					}
				}
			}
		case *ast.LetClause:
			lets = append(lets, expectedLet{name: d.Ident.Name, expr: d.Expr})
		case *ast.Field:
			// Separate non-@test attributes from @test attributes.
			// Detect @test(final), @test(ignore), and @test(err) on individual fields.
			var nonTestAttrs []*ast.Attribute
			isFinal := false
			isIgnore := false
			var errCk *errArgs
			for _, a := range d.Attrs {
				if k, _ := a.Split(); k == "test" {
					pa, err := parseTestAttr(a)
					if err == nil {
						switch pa.directive {
						case "final":
							isFinal = true
						case "ignore":
							isIgnore = true
						case "err":
							errCk = pa.errArgs
							if errCk == nil {
								errCk = &errArgs{} // bare @test(err)
							}
						}
					}
				} else {
					nonTestAttrs = append(nonTestAttrs, a)
				}
			}
			constraint := d.Constraint
			switch label := d.Label.(type) {
			case *ast.Ident:
				name := label.Name
				isDef := strings.HasPrefix(name, "#") || strings.HasPrefix(name, "_#")
				isHidden := !isDef && strings.HasPrefix(name, "_")
				fields = append(fields, expectedField{
					name:       name,
					isDef:      isDef,
					isHidden:   isHidden,
					constraint: constraint,
					final:      isFinal,
					ignore:     isIgnore,
					errCheck:   errCk,
					value:      d.Value,
					attrs:      nonTestAttrs,
				})
			case *ast.BasicLit:
				// String label like "foo".
				s, err := literal.Unquote(label.Value)
				if err != nil {
					return pathErr(path, "cannot unquote label %q: %v", label.Value, err)
				}
				fields = append(fields, expectedField{
					name:       s,
					constraint: constraint,
					final:      isFinal,
					ignore:     isIgnore,
					errCheck:   errCk,
					value:      d.Value,
					attrs:      nonTestAttrs,
				})
			case *ast.ListLit:
				// Pattern constraint [expr]: value.
				if len(label.Elts) != 1 {
					return pathErr(path, "pattern constraint label must have exactly one element")
				}
				patterns = append(patterns, expectedPattern{label: label.Elts[0], value: d.Value})
			}
		case *ast.EmbedDecl:
			// Embeddings are collected for non-struct value handling below.
		}
	}

	// Check if any field has @test(err), which means the struct may contain
	// errors that cause IncompleteKind() to return _|_ instead of StructKind.
	hasErrCheck := false
	for _, ef := range fields {
		if ef.errCheck != nil {
			hasErrCheck = true
			break
		}
	}

	// If the value is not a struct and @test(final) is set with no fields,
	// unwrap the single embedding and compare directly, skipping structural
	// consistency check. This allows {int, @test(final)} to match int & >=0.
	if val.IncompleteKind() != cue.StructKind && !hasErrCheck {
		if !allFinal {
			return pathErr(path, "expected struct, got %v", val.IncompleteKind())
		}
		var embeds []ast.Expr
		for _, d := range s.Elts {
			if e, ok := d.(*ast.EmbedDecl); ok {
				embeds = append(embeds, e.Expr)
			}
		}
		if len(embeds) != 1 || len(fields) != 0 {
			return pathErr(path, "expected struct, got %v", val.IncompleteKind())
		}
		return cmpFinal(path, embeds[0], val)
	}

	// Compare regular fields (including definitions, optional, required, hidden).
	type seenKey struct {
		name       string
		constraint token.Token
	}
	seen := make(map[seenKey]bool, len(fields))
	for _, ef := range fields {
		seen[seenKey{ef.name, ef.constraint}] = true
		sel := fieldSelector(ef.name, ef.isDef, ef.isHidden, ef.constraint)
		child := val.LookupPath(cue.MakePath(sel))

		// @test(ignore): skip eq descent; field need not be present.
		// Other directives (e.g. @test(err)) still run if the field exists.
		if ef.ignore {
			if ef.errCheck != nil && child.Exists() {
				if err := c.cmpErr(append(path, ef.name), child, ef.errCheck); err != nil {
					return err
				}
			}
			continue
		}

		if !child.Exists() {
			return pathErr(path, "field %q not found in value", ef.name)
		}

		fieldPath := append(path, ef.name)

		// @test(err): check value is an error instead of normal comparison.
		if ef.errCheck != nil {
			if err := c.cmpErr(fieldPath, child, ef.errCheck); err != nil {
				return err
			}
			continue
		}

		cmpChild := child
		isFinal := ef.final || allFinal
		if isFinal {
			// @test(final): resolve the default value before comparing,
			// and skip the structural disjunction/conjunction check.
			if d, ok := child.Default(); ok {
				cmpChild = d
			}
		}
		var cmpErr error
		if isFinal {
			cmpErr = cmpFinal(fieldPath, ef.value, cmpChild)
		} else {
			cmpErr = c.astCmp(fieldPath, ef.value, cmpChild)
		}
		if cmpErr != nil {
			return cmpErr
		}
		// Check attributes: order-independent matching, supporting
		// multiple attributes with the same key (e.g. @foo() @foo(other)).
		if err := cmpFieldAttrs(append(path, ef.name), ef.attrs, child); err != nil {
			return err
		}
	}

	// Check for unexpected fields in the value.
	opts := []cue.Option{cue.Definitions(true), cue.Hidden(true), cue.Optional(true)}
	iter, err := val.Fields(opts...)
	if err != nil {
		return pathErr(path, "cannot iterate value fields: %v", err)
	}
	var valFieldOrder []string
	for iter.Next() {
		sel := iter.Selector()
		if sel.IsConstraint() && sel.ConstraintType() == cue.PatternConstraint {
			continue // skip pattern constraints here
		}
		var name string
		if sel.IsString() {
			name = sel.Unquoted()
		} else {
			name = sel.String()
		}
		valFieldOrder = append(valFieldOrder, name)
		constraint := selConstraintToken(sel)
		if !seen[seenKey{name, constraint}] {
			return pathErr(path, "unexpected field %q in value", name)
		}
	}

	// Check field ordering if @test(checkOrder) was present.
	if checkOrder {
		astOrder := make([]string, len(fields))
		for i, f := range fields {
			astOrder[i] = f.name
		}
		if len(astOrder) != len(valFieldOrder) {
			return pathErr(path, "checkOrder: field count mismatch: expected %d, got %d",
				len(astOrder), len(valFieldOrder))
		}
		for i := range astOrder {
			if astOrder[i] != valFieldOrder[i] {
				return pathErr(path, "checkOrder: field %d: expected %q, got %q",
					i, astOrder[i], valFieldOrder[i])
			}
		}
	}

	// Compare pattern constraints.
	if len(patterns) > 0 {
		patIter, err := val.Fields(cue.Patterns(true))
		if err != nil {
			return pathErr(path, "cannot iterate value patterns: %v", err)
		}
		var valPatterns []struct {
			sel   cue.Selector
			value cue.Value
		}
		for patIter.Next() {
			sel := patIter.Selector()
			if !sel.IsConstraint() {
				continue
			}
			valPatterns = append(valPatterns, struct {
				sel   cue.Selector
				value cue.Value
			}{sel, patIter.Value()})
		}

		if len(valPatterns) != len(patterns) {
			return pathErr(path, "expected %d pattern constraint(s), got %d",
				len(patterns), len(valPatterns))
		}
		// Match patterns positionally for now (both ordered by source).
		for i, ep := range patterns {
			vp := valPatterns[i]
			patPath := append(path, fmt.Sprintf("[%d]pattern", i))
			if err := c.astCmp(patPath, ep.value, vp.value); err != nil {
				return err
			}
		}
	}

	// Compare let bindings listed in the expected struct.
	// Each expected let must be present in the evaluated vertex with a matching value.
	if len(lets) > 0 {
		opCtx := value.OpContext(val)
		v := value.Vertex(val)

		// Build a map from let base-name to arc.
		valLets := make(map[string]*adt.Vertex, len(v.Arcs))
		for _, arc := range v.Arcs {
			if arc.Label.IsLet() {
				name := arc.Label.IdentString(opCtx)
				valLets[name] = arc
			}
		}

		for _, el := range lets {
			arc, ok := valLets[el.name]
			if !ok {
				return pathErr(path, "let binding %q not found in value", el.name)
			}
			letPath := append(path, "let "+el.name)
			letVal := value.Make(opCtx, arc)
			if err := c.astCmp(letPath, el.expr, letVal); err != nil {
				return err
			}
		}
	}

	return nil
}

// ── attribute comparison ────────────────────────────────────────────────────

// cmpFieldAttrs compares expected AST attributes against the actual attributes
// on a cue.Value field. Matching is order-independent and supports multiple
// attributes with the same key (e.g. @foo() @foo(other)).
// Both missing and unexpected attributes are reported as errors.
func cmpFieldAttrs(path []string, expected []*ast.Attribute, child cue.Value) error {
	// Collect value attributes as key:body pairs.
	type attrEntry struct {
		key  string
		body string
	}
	var valAttrs []attrEntry
	for _, a := range export.ExtractFieldAttrs(child.Core().V) {
		k, body := a.Split()
		if k == "test" {
			continue // skip @test directives
		}
		valAttrs = append(valAttrs, attrEntry{k, body})
	}

	// Order-independent matching: each expected attr must match exactly one
	// value attr, and vice versa.
	matched := make([]bool, len(valAttrs))
	for _, a := range expected {
		k, body := a.Split()
		found := false
		for j, va := range valAttrs {
			if matched[j] {
				continue
			}
			if va.key == k && va.body == body {
				matched[j] = true
				found = true
				break
			}
		}
		if !found {
			return pathErr(path, "expected attribute @%s(%s), not found in value", k, body)
		}
	}
	// Check for unexpected value attributes.
	for j, va := range valAttrs {
		if !matched[j] {
			return pathErr(path, "unexpected attribute @%s(%s) in value", va.key, va.body)
		}
	}
	return nil
}

// ── error assertions ────────────────────────────────────────────────────────

// cmpErr checks that val is an error, optionally validating error code and
// positions. This implements @test(err, ...) on fields within an expected struct.
//
// Position specs use absolute line numbers (not deltas) since nested @test(err)
// attributes have no source line to be relative to.
func (c *cmpCtx) cmpErr(path []string, val cue.Value, ea *errArgs) error {
	core := val.Core()
	if core.V == nil {
		return pathErr(path, "@test(err): value has no vertex")
	}
	b := core.V.Bottom()
	if b == nil {
		return pathErr(path, "@test(err): expected error, got non-error value")
	}
	if len(ea.codes) > 0 {
		gotCode := b.Code.String()
		if !ea.matchesCode(gotCode) {
			return pathErr(path, "@test(err): expected error code %v, got %q", ea.codes, gotCode)
		}
	}
	// TODO: support CUE_UPDATE=1 and CUE_UPDATE=force for nested pos= specs.
	// This requires rewriting text inside the outer @test(eq, {...}) attribute
	// body, which the current enqueuePosWrite machinery doesn't handle.
	if ea.posSet {
		err := val.Err()
		if err == nil {
			return pathErr(path, "@test(err, pos=...): value has no error")
		}
		positions := cueerrors.Positions(err)
		if len(positions) != len(ea.pos) {
			var got []string
			for _, p := range positions {
				got = append(got, fmt.Sprintf("%d:%d", p.Line(), p.Column()))
			}
			return pathErr(path, "@test(err, pos=...): got %d position(s) %v, want %d",
				len(positions), got, len(ea.pos))
		}
		// Order-independent matching: each expected position must match
		// exactly one actual position.
		matched := make([]bool, len(positions))
		for _, exp := range ea.pos {
			found := false
			for j, got := range positions {
				if matched[j] {
					continue
				}
				if exp.fileName != "" {
					if got.Filename() == exp.fileName && got.Line() == exp.absLine && got.Column() == exp.col {
						matched[j] = true
						found = true
						break
					}
				} else {
					wantLine := c.baseLine + exp.deltaLine
					if got.Line() == wantLine && got.Column() == exp.col {
						matched[j] = true
						found = true
						break
					}
				}
			}
			if !found {
				var got []string
				for _, p := range positions {
					got = append(got, fmt.Sprintf("%d:%d", p.Line(), p.Column()))
				}
				wantLine := c.baseLine + exp.deltaLine
				return pathErr(path, "@test(err, pos=...): no match for expected position %d:%d in %v",
					wantLine, exp.col, got)
			}
		}
	}
	return nil
}

// ── lists ───────────────────────────────────────────────────────────────────

func (c *cmpCtx) cmpList(path []string, l *ast.ListLit, val cue.Value) error {
	if val.IncompleteKind() != cue.ListKind {
		return pathErr(path, "expected list, got %v", val.IncompleteKind())
	}
	iter, err := val.List()
	if err != nil {
		return pathErr(path, "cannot iterate value list: %v", err)
	}
	i := 0
	for ; i < len(l.Elts) && iter.Next(); i++ {
		elemPath := append(path, fmt.Sprintf("[%d]", i))
		if err := c.astCmp(elemPath, l.Elts[i], iter.Value()); err != nil {
			return err
		}
	}
	if i < len(l.Elts) {
		return pathErr(path, "list: expected %d elements, value has %d", len(l.Elts), i)
	}
	// Count remaining value elements.
	extra := 0
	for iter.Next() {
		extra++
	}
	if extra > 0 {
		return pathErr(path, "list: expected %d elements, value has %d", len(l.Elts), i+extra)
	}
	return nil
}

// ── disjunctions ────────────────────────────────────────────────────────────

// astDisjunct holds a single disjunct extracted from the AST.
type astDisjunct struct {
	expr      ast.Expr
	isDefault bool
}

// flattenDisjunction recursively collects disjuncts from nested OR
// expressions, tracking default markers (*expr).
func flattenDisjunction(expr ast.Expr) []astDisjunct {
	switch e := expr.(type) {
	case *ast.BinaryExpr:
		if e.Op == token.OR {
			return append(flattenDisjunction(e.X), flattenDisjunction(e.Y)...)
		}
	case *ast.UnaryExpr:
		if e.Op == token.MUL {
			ds := flattenDisjunction(e.X)
			for i := range ds {
				ds[i].isDefault = true
			}
			return ds
		}
	case *ast.ParenExpr:
		return flattenDisjunction(e.X)
	}
	return []astDisjunct{{expr: expr}}
}

func (c *cmpCtx) cmpDisjunction(path []string, expr ast.Expr, val cue.Value) error {
	astDs := flattenDisjunction(expr)

	// Get disjuncts from the value via Expr().
	tv := val.Core()
	dj, ok := tv.V.BaseValue.(*adt.Disjunction)
	if !ok {
		return pathErr(path, "value is a disjunction but expected expression is not; "+
			"use a disjunction in the expected value or @test(final) on the field")
	}

	// Match value disjuncts to AST disjuncts order-independently.
	// Each value disjunct must match exactly one AST disjunct.
	if len(dj.Values) != len(astDs) {
		return pathErr(path, "disjunction: expected %d disjunct(s), got %d",
			len(astDs), len(dj.Values))
	}

	// Separate AST disjuncts into defaults and non-defaults.
	var astDefaults, astNonDefaults []ast.Expr
	for _, d := range astDs {
		if d.isDefault {
			astDefaults = append(astDefaults, d.expr)
		} else {
			astNonDefaults = append(astNonDefaults, d.expr)
		}
	}

	vDefs := dj.Values[:dj.NumDefaults]
	vNonDefs := dj.Values[dj.NumDefaults:]

	// Check count agreement between AST and value for defaults and non-defaults.
	if len(astDefaults) != len(vDefs) {
		return pathErr(path, "disjunction: expected %d default(s), got %d",
			len(astDefaults), len(vDefs))
	}
	if len(astNonDefaults) != len(vNonDefs) {
		return pathErr(path, "disjunction: expected %d non-default(s), got %d",
			len(astNonDefaults), len(vNonDefs))
	}

	// Order-independent matching: defaults against defaults.
	opCtx := value.OpContext(val)
	if err := c.matchDisjuncts(path, opCtx, "default", astDefaults, vDefs); err != nil {
		return err
	}
	// Non-defaults against non-defaults.
	return c.matchDisjuncts(path, opCtx, "non-default", astNonDefaults, vNonDefs)
}

// matchDisjuncts performs order-independent matching of AST expressions
// against value disjuncts. kind is "default" or "non-default" for error
// messages.
func (c *cmpCtx) matchDisjuncts(path []string, opCtx *adt.OpContext, kind string, astExprs []ast.Expr, vals []adt.Value) error {
	matched := make([]bool, len(vals))
	for _, ae := range astExprs {
		found := false
		for j, v := range vals {
			if matched[j] {
				continue
			}
			if c.astCmp(path, ae, value.Make(opCtx, v)) == nil {
				matched[j] = true
				found = true
				break
			}
		}
		if !found {
			return pathErr(path, "disjunction: no matching %s disjunct for AST expr at %s", kind, pos(ae))
		}
	}
	return nil
}

// flattenConjunction collects all conjuncts from nested & expressions.
func flattenConjunction(expr ast.Expr) []ast.Expr {
	if e, ok := expr.(*ast.BinaryExpr); ok && e.Op == token.AND {
		return append(flattenConjunction(e.X), flattenConjunction(e.Y)...)
	}
	if e, ok := expr.(*ast.ParenExpr); ok {
		return flattenConjunction(e.X)
	}
	return []ast.Expr{expr}
}

func (c *cmpCtx) cmpConjunction(path []string, e *ast.BinaryExpr, val cue.Value) error {
	astParts := flattenConjunction(e)
	op, args := val.Eval().Expr()
	if op != cue.AndOp {
		return pathErr(path, "expected conjunction (&), got %v", op)
	}
	if len(args) != len(astParts) {
		return pathErr(path, "conjunction: expected %d conjunct(s), got %d",
			len(astParts), len(args))
	}
	// Order-independent matching.
	matched := make([]bool, len(args))
	for _, ae := range astParts {
		found := false
		for j, vd := range args {
			if matched[j] {
				continue
			}
			if c.astCmp(path, ae, vd) == nil {
				matched[j] = true
				found = true
				break
			}
		}
		if !found {
			return pathErr(path, "conjunction: no matching value conjunct for AST expr at %s", pos(ae))
		}
	}
	return nil
}

// ── unary expressions (non-default) ────────────────────────────────────────

func (c *cmpCtx) cmpUnaryExpr(path []string, e *ast.UnaryExpr, val cue.Value) error {
	op, args := val.Eval().Expr()
	if len(args) != 1 {
		return pathErr(path, "expected unary %v, got op=%v with %d args", e.Op, op, len(args))
	}
	wantOp := tokenToOp(e.Op)
	if op != wantOp {
		return pathErr(path, "expected op %v, got %v", wantOp, op)
	}
	return c.astCmp(path, e.X, args[0])
}

// ── identifiers (types and special values) ──────────────────────────────────

func cmpIdent(path []string, id *ast.Ident, val cue.Value) error {
	switch id.Name {

	// Bottom: value must be an error.
	case "_|_":
		if val.Err() == nil {
			return pathErr(path, "expected bottom (_|_), got %v", val)
		}
		return nil

	default:
		return cmpFinal(path, id, val)
	}
}

// ── field selector helpers ──────────────────────────────────────────────────

// fieldSelector returns the cue.Selector for looking up a field based on its
// name, definition/hidden status, and constraint type (optional/required).
func fieldSelector(name string, isDef, isHidden bool, constraint token.Token) cue.Selector {
	var sel cue.Selector
	switch {
	case isDef:
		sel = cue.Def(name)
	case isHidden:
		sel = cue.Hid(name, "_")
	default:
		sel = cue.Str(name)
	}
	switch constraint {
	case token.OPTION:
		sel = sel.Optional()
	case token.NOT:
		sel = sel.Required()
	}
	return sel
}

// selConstraintToken returns the ast token.Token corresponding to the
// constraint type of a cue.Selector.
func selConstraintToken(sel cue.Selector) token.Token {
	switch sel.ConstraintType() {
	case cue.OptionalConstraint:
		return token.OPTION
	case cue.RequiredConstraint:
		return token.NOT
	default:
		return token.ILLEGAL
	}
}

// ── helpers ─────────────────────────────────────────────────────────────────

func pathErr(path []string, format string, args ...any) error {
	prefix := strings.Join(path, ".")
	if prefix == "" {
		return fmt.Errorf(format, args...)
	}
	return fmt.Errorf("%s: "+format, append([]any{prefix}, args...)...)
}

func pos(n ast.Node) token.Pos {
	return n.Pos()
}

// tokenToOp converts an ast token to the cue.Op equivalent.
func tokenToOp(t token.Token) cue.Op {
	switch t {
	case token.ADD:
		return cue.AddOp
	case token.SUB:
		return cue.SubtractOp
	case token.MUL:
		return cue.MultiplyOp
	case token.QUO:
		return cue.FloatQuotientOp
	case token.LSS:
		return cue.LessThanOp
	case token.LEQ:
		return cue.LessThanEqualOp
	case token.GTR:
		return cue.GreaterThanOp
	case token.GEQ:
		return cue.GreaterThanEqualOp
	case token.NEQ:
		return cue.NotEqualOp
	case token.MAT:
		return cue.RegexMatchOp
	case token.NMAT:
		return cue.NotRegexMatchOp
	default:
		return cue.NoOp
	}
}
