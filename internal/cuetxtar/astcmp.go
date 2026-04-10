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
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
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
	return (&cmpCtx{}).astCmp(cue.Path{}, expr, val)
}

// astCmp is the recursive comparison workhorse. path accumulates the
// human-readable location for error messages.
//
// It checks structural consistency: if the value is a disjunction but the
// expected AST is not (or vice versa for conjunctions), an error is reported.
// Use @test(final) on a field in the expected struct to opt into
// default-resolution, allowing a plain value to match a disjunction default.
func (c *cmpCtx) astCmp(path cue.Path, expr ast.Expr, val cue.Value) error {
	// Structural consistency check: if the value is a disjunction or
	// conjunction, the expected AST must reflect that structure.
	if err := checkStructuralMatch(path, expr, val); err != nil {
		return err
	}

	switch e := expr.(type) {
	case *ast.StructLit:
		return c.cmpStruct(path, e, val)
	case *ast.ListLit:
		if err := checkNoExtraFields(path, val); err != nil {
			return err
		}
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
		if err := checkNoExtraFields(path, val); err != nil {
			return err
		}
		return cmpFinal(path, e, val)
	case *ast.Ident:
		if err := checkNoExtraFields(path, val); err != nil {
			return err
		}
		return cmpIdent(path, e, val)
	case *ast.ParenExpr:
		return c.astCmp(path, e.X, val)
	case *ast.BottomLit:
		return nil
	case *ast.CallExpr, *ast.SelectorExpr:
		return cmpBuiltinExpr(path, expr, val)
	}
	return pathErr(path, "unsupported AST node type %T", expr)
}

// checkNoExtraFields reports an error if val has any hidden or definition
// fields that would be silently ignored when the expected expression is a
// non-struct (e.g. a list literal or scalar). In those cases the caller
// must use a struct-form expected value to make hidden fields explicit:
//
//	@test(eq, {_foo: "foo", ["bar"]})
func checkNoExtraFields(path cue.Path, val cue.Value) error {
	iter, err := val.Fields(cue.Definitions(true), cue.Hidden(true))
	if err != nil {
		// Not a struct — no extra fields possible.
		return nil
	}
	for iter.Next() {
		name := iter.Selector().String()
		// Skip list-element index selectors (e.g. "0", "1") that arise when
		// val is a struct with an embedded list — those are part of the list
		// value itself, not extra struct fields.
		if !internal.IsDefOrHidden(name) {
			continue
		}
		return pathErr(path, "value has field %q not present in the non-struct"+
			" expected expression; use a struct form, e.g. @test(eq, {%s: ..., ...})",
			name, name)
	}
	return nil
}

// cmpFinal compiles the AST expression and checks that the compiled value
// equals val. This is the @test(final) path: it opts out of structural
// AST comparison in favor of compile-and-compare. Defaults are resolved
// on val before the check.
func cmpFinal(path cue.Path, expr ast.Expr, val cue.Value) error {
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
		return pathErr(path, "expected %s, got %#v", b, val)
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
func checkStructuralMatch(path cue.Path, expr ast.Expr, val cue.Value) error {
	if val.IsConcrete() {
		return nil
	}
	// Struct literals may contain @test(final) which handles mismatches
	// internally via cmpFinal, so defer the check to cmpStruct.
	if _, ok := expr.(*ast.StructLit); ok {
		return nil
	}
	tv := val.Core()
	switch tv.V.DerefValue().BaseValue.(type) {
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

func (c *cmpCtx) cmpStruct(path cue.Path, s *ast.StructLit, val cue.Value) error {
	// Collect expected fields, let bindings, and pattern constraints from the AST.
	// Also detect @test(checkOrder), @test(final), and non-@test attributes.
	type expectedField struct {
		name     string       // raw field name (for errors and checkOrder)
		sel      cue.Selector // ready-to-use selector (encodes kind, pkg, constraint)
		final    bool         // @test(final): resolve default before comparing
		ignore   bool         // @test(ignore): skip eq descent; field need not exist
		errCheck *errArgs     // @test(err, ...): check value is error instead of comparing
		value    ast.Expr
		attrs    []*ast.Attribute // non-@test attributes
	}
	type expectedLet struct {
		name string
		expr ast.Expr
	}
	type expectedPattern struct {
		pattern ast.Expr // the expression inside [...]
		value   ast.Expr
	}

	var fields []expectedField
	var lets []expectedLet
	var patterns []expectedPattern
	checkOrder := false
	allFinal := false
	hasEmbed := false
	seenShareIDs := make(map[string]bool)

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
			// Detect @test(final), @test(ignore), @test(err), and @test(shareID=name)
			// on individual fields.
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
						case "shareID":
							// The first field with a given shareID name runs the eq check
							// normally so every value-expr pair is checked at least once.
							// Subsequent fields with the same shareID name skip the eq
							// check; sharing is verified separately by runShareIDChecks.
							if len(pa.raw.Fields) > 0 {
								name := pa.raw.Fields[0].Value()
								if seenShareIDs[name] {
									isIgnore = true
								} else {
									seenShareIDs[name] = true
								}
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
				// For hidden fields, a $pkg suffix specifies the package scope.
				// e.g. _foo$mypkg means hidden field _foo scoped to package mypkg,
				// stored as ":mypkg" in inline-compiled sources (colon-prefixed).
				var sel cue.Selector
				if !internal.IsHidden(name) {
					sel = cue.Label(label)
				} else {
					pkg := "_"
					if i := strings.IndexByte(name, '$'); i >= 0 {
						pkg = ":" + name[i+1:]
						name = name[:i]
					}
					sel = cue.Hid(name, pkg)
				}
				sel = applyConstraint(sel, constraint)
				fields = append(fields, expectedField{
					name:     name,
					sel:      sel,
					final:    isFinal,
					ignore:   isIgnore,
					errCheck: errCk,
					value:    d.Value,
					attrs:    nonTestAttrs,
				})
			case *ast.BasicLit:
				sel := cue.Label(label)
				fields = append(fields, expectedField{
					name:     sel.String(),
					sel:      applyConstraint(sel, constraint),
					final:    isFinal,
					ignore:   isIgnore,
					errCheck: errCk,
					value:    d.Value,
					attrs:    nonTestAttrs,
				})
			case *ast.ListLit:
				// Pattern constraint [expr]: value.
				if len(label.Elts) != 1 {
					return pathErr(path, "pattern constraint label must have exactly one element")
				}
				patterns = append(patterns, expectedPattern{pattern: label.Elts[0], value: d.Value})
			}
		case *ast.EmbedDecl:
			hasEmbed = true
			if err := c.cmpEmbedExpr(path, d.Expr, val); err != nil {
				return err
			}
			// Embeddings are collected for non-struct value handling below.
		}
	}

	// When the expected struct has no embedded expression, verify that the actual
	// value also carries no embedded scalar or type constraint. Without this check,
	// omitting an embed from the expected value silently passes (e.g. writing
	// {_#cond: true} when the actual value is {5, _#cond: true}).
	if !hasEmbed {
		if scalar, ok := embeddedScalar(val); ok {
			return pathErr(path, "value has embedded %v but expected struct has no embedded expression",
				scalar)
		}
	}

	// Compare regular fields (including definitions, optional, required, hidden).
	seen := make(map[cue.Selector]bool, len(fields))
	for _, ef := range fields {
		seen[ef.sel] = true
		child := val.LookupPath(cue.MakePath(ef.sel))

		fieldPath := path.Append(ef.sel)

		// @test(ignore): skip eq descent; field need not be present.
		// Other directives (e.g. @test(err)) still run if the field exists.
		if ef.ignore {
			if ef.errCheck != nil && child.Exists() {
				if err := c.cmpErr(fieldPath, child, ef.errCheck); err != nil {
					return err
				}
			}
			continue
		}

		if !child.Exists() {
			return pathErr(path, "field %q not found in value", ef.name)
		}

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
		if err := cmpFieldAttrs(fieldPath, ef.attrs, child); err != nil {
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
		// When the expected struct embeds a non-struct value (list or scalar),
		// Fields() may also return list-element index selectors (e.g. "0", "1").
		// These are part of the embedded base value and should not be flagged
		// as unexpected — skip them when there was an EmbedDecl in the expected.
		if hasEmbed && !sel.IsString() && !internal.IsDefOrHidden(name) {
			continue
		}
		// For hidden fields the expected struct may use bare _foo (pkg="_")
		// while the value stores _foo scoped to a package. Accept either.
		if !seen[sel] {
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
		type valPat struct {
			sel   cue.Selector
			value cue.Value
		}
		var valPatterns []valPat
		for patIter.Next() {
			sel := patIter.Selector()
			if !sel.IsConstraint() {
				continue
			}
			valPatterns = append(valPatterns, valPat{sel, patIter.Value()})
		}

		if len(valPatterns) != len(patterns) {
			return pathErr(path, "expected %d pattern constraint(s), got %d",
				len(patterns), len(valPatterns))
		}

		// Match patterns positionally (both ordered by source).
		for _, ep := range patterns {
			patPath := path.Append(cue.Str(fmt.Sprintf("[%s]", toStr(ep.pattern))))
			found := false
			var vp valPat
			for _, p := range valPatterns {
				if err := c.astCmp(patPath, ep.pattern, p.sel.Pattern()); err == nil {
					found = true
					vp = p
					break
				}
			}
			if !found {
				return pathErr(patPath, "pattern constraint %v not found in value", toStr(ep.pattern))
			}

			if err := c.astCmp(patPath, ep.value, vp.value); err != nil {
				return pathErr(patPath, "incompatible pattern value: %w", err)
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
			letPath := path.Append(cue.Str("let " + el.name))
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
func cmpFieldAttrs(path cue.Path, expected []*ast.Attribute, child cue.Value) error {
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
func (c *cmpCtx) cmpErr(path cue.Path, val cue.Value, ea *errArgs) error {
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
			msg := formatPosCountMismatch("@test(err, pos=...)", len(positions), len(ea.pos))
			return pathErr(path, "%s %v", msg, got)
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

func (c *cmpCtx) cmpList(path cue.Path, l *ast.ListLit, val cue.Value) error {
	if val.IncompleteKind() != cue.ListKind {
		return pathErr(path, "expected list, got %v", val.IncompleteKind())
	}
	iter, err := val.List()
	if err != nil {
		return pathErr(path, "cannot iterate value list: %v", err)
	}
	i := 0
	for ; i < len(l.Elts) && iter.Next(); i++ {
		elemPath := path.Append(cue.Index(i))
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

func (c *cmpCtx) cmpDisjunction(path cue.Path, expr ast.Expr, val cue.Value) error {
	astDs := flattenDisjunction(expr)

	// Get disjuncts from the value via Expr().
	// Use DerefValue() to follow sharing/forwarding pointers to the actual vertex.
	tv := val.Core()
	dj, ok := tv.V.DerefValue().BaseValue.(*adt.Disjunction)
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
func (c *cmpCtx) matchDisjuncts(path cue.Path, opCtx *adt.OpContext, kind string, astExprs []ast.Expr, vals []adt.Value) error {
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
			return pathErr(path, "%s: no matching %s disjunct %s", pos(ae), kind, toStr(ae))
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

func (c *cmpCtx) cmpConjunction(path cue.Path, e *ast.BinaryExpr, val cue.Value) error {
	// Check that this is a valid conjunction. If it is, we can investigate
	// the conjuncts in isolation as per how lattices work.
	ctx := val.Context()
	v := ctx.BuildExpr(e, cue.InferBuiltins(true))
	if err := v.Err(); err != nil {
		return pathErr(path, "cannot compile conjunction expression: %v", err)
	}

	astParts := flattenConjunction(e)
	op, args := val.Eval().Expr()
	if op != cue.AndOp && op != cue.NoOp {
		// The conjunction may have been simplified during evaluation
		// (e.g. struct.MaxFields(2) & {} evaluates to a plain struct value).
		// We could be comparing a struct and a struct and a validator.
		return pathErr(path, "expected conjunction (&), got %v", op)
	}
	if len(args) != len(astParts) && len(args) != len(astParts)-1 {
		return pathErr(path, "conjunction: expected %d conjunct(s), got %d",
			len(astParts), len(args))
	}
	// Order-independent matching.
	matched := make([]bool, len(args))
	for _, ae := range astParts {
		found := false
		if s, ok := ae.(*ast.StructLit); ok {
			if err := c.cmpStruct(path, s, val); err != nil {
				return err
			}
			continue
		}
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
			// If the value has one fewer conjunct than expected, a validator
			// was consumed during evaluation. Accept the unmatched non-struct
			// conjunct — it documents the constraint that was applied.
			if len(args) < len(astParts) {
				continue
			}
			return pathErr(path, "%s: no matching conjunct expr %s", pos(ae), toStr(ae))
		}
	}
	return nil
}

// ── unary expressions (non-default) ────────────────────────────────────────

func (c *cmpCtx) cmpUnaryExpr(path cue.Path, e *ast.UnaryExpr, val cue.Value) error {
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

func toStr(expr ast.Expr) string {
	b, err := format.Node(expr)
	if err != nil {
		return fmt.Sprintf("cannot format expression: %v", err)
	}
	return string(b)
}

// ── builtin call and selector expressions ───────────────────────────────────

// cmpBuiltinExpr compiles expr using InferBuiltins so that unresolved
// package-qualified references (e.g. struct.MaxFields(2), math.Pi) resolve to
// CUE builtin packages, then compares the resulting value against val.
func cmpBuiltinExpr(path cue.Path, expr ast.Expr, val cue.Value) error {
	ctx := val.Context()
	expected := ctx.BuildExpr(expr, cue.InferBuiltins(true))
	if err := expected.Err(); err != nil {
		return pathErr(path, "cannot compile expression %s: %v", toStr(expr), err)
	}
	if !valuesEqual(expected, val) {
		return pathErr(path, "expected %s, got %v", toStr(expr), val)
	}
	return nil
}

// ── identifiers (types and special values) ──────────────────────────────────

func cmpIdent(path cue.Path, id *ast.Ident, val cue.Value) error {
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

// applyConstraint wraps sel with Optional or Required based on the constraint token.
func applyConstraint(sel cue.Selector, constraint token.Token) cue.Selector {
	switch constraint {
	case token.OPTION:
		return sel.Optional()
	case token.NOT:
		return sel.Required()
	}
	return sel
}

// ── helpers ─────────────────────────────────────────────────────────────────

// cmpEmbedExpr compares an embedded expression from an expected struct against
// the actual value. It bypasses checkNoExtraFields because the outer cmpStruct
// already validates all struct-level fields via its seen-set check.
//
// Lists are compared directly via cmpList. Scalars are extracted first via
// embeddedScalar so that astCmp sees a pure scalar rather than the surrounding
// struct arcs.
func (c *cmpCtx) cmpEmbedExpr(path cue.Path, expr ast.Expr, val cue.Value) error {
	if listExpr, ok := expr.(*ast.ListLit); ok {
		return c.cmpList(path, listExpr, val)
	}
	innerVal := val
	if scalar, ok := embeddedScalar(val); ok {
		innerVal = scalar
	}
	return c.astCmp(path, expr, innerVal)
}

// embeddedScalar extracts the embedded non-struct, non-list BaseValue from a
// vertex value, following vertex indirections that arise with structure sharing.
// Returns (scalar, true) when the base value is an adt.Value that is not a
// further Vertex (e.g. a concrete int, a type constraint like string, a bound).
// Returns (zero, false) when the base value is a StructMarker, ListMarker, or nil.
func embeddedScalar(val cue.Value) (cue.Value, bool) {
	for v := val.Core().V; v != nil; {
		bv, ok := v.BaseValue.(adt.Value)
		if !ok {
			return cue.Value{}, false // *StructMarker or *ListMarker
		}
		if _, isBottom := bv.(*adt.Bottom); isBottom {
			return cue.Value{}, false // error base value — not an embedded scalar
		}
		if vertex, ok2 := bv.(*adt.Vertex); ok2 {
			v = vertex // follow structure-sharing indirection
			continue
		}
		return value.Make(value.OpContext(val), bv), true
	}
	return cue.Value{}, false
}

func pathErr(path cue.Path, format string, args ...any) error {
	prefix := path.String()
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
