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

// Package hover computes value-based hover content for LSP requests:
// given a cursor position, it produces a synthetic expression showing
// the value that position denotes, unified across all of its
// declarations, with references inlined. For example, given:
//
//	y: 5
//	x: y
//	z: int
//	x: z
//
// hovering anywhere within a declaration of x yields the expression
// `5 & int`.
package hover

import (
	"cmp"
	"slices"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/unstable/lsp/eval"
)

// nodeBudget caps the total number of AST nodes constructed while
// rendering one unified value: reference inlining can pull in
// arbitrarily large expressions, and beyond this size the hover
// stops being informative.
const nodeBudget = 256

// maxInlineDepth caps the output nesting depth (enclosing structs and
// lists, whether written in the source or created by earlier
// inlining) at which references are still replaced by what they refer
// to; deeper references are left as written. This spends [nodeBudget]
// on breadth near the top of the value rather than on one deep spine,
// and a reference left in place is itself hoverable, so deeper detail
// remains one hover away.
const maxInlineDepth = 3

// budgetExceeded is panicked by [renderer.countNode] to abandon a
// rendering that has grown past [nodeBudget]; [ValueForOffset]
// recovers it.
type budgetExceeded struct{}

// ValueForOffset returns a synthetic expression showing the unified
// value at the given file offset (number of bytes from the start of
// the file). The expression is nil if the offset does not denote a
// value, or when tooBig is true: rendering was abandoned because the
// result exceeded [nodeBudget] nodes.
//
// If the offset is within a field's declaration — its label, or
// anywhere in its value at "unification level" — the returned
// expression shows the field's value across all of its declarations,
// with references replaced by the values they refer to, recursively.
// If instead the offset sits inside a sub-expression whose value is
// never unified with the field's other declarations (a call argument,
// say), there is no unified value to show; but a reference at the
// offset still shows the value of whatever it refers to: hovering the
// a in `x: f(a)` shows a's value.
//
// The returned expression shares no nodes with any source AST, and
// carries no position information and no comments.
func ValueForOffset(fe *eval.FileEvaluator, offset int) (expr ast.Expr, tooBig bool) {
	defer func() {
		if e := recover(); e != nil {
			if _, ok := e.(budgetExceeded); !ok {
				panic(e)
			}
			expr, tooBig = nil, true
		}
	}()

	tokFile := fe.File.Pos().File()

	var targets eval.NodeSet
	seen := make(map[*eval.Node]bool)
	addTargets := func(ns ...*eval.Node) {
		for _, n := range ns {
			if !seen[n] {
				seen[n] = true
				targets = append(targets, n)
			}
		}
	}

	for _, d := range fe.DeclsForOffset(offset) {
		// Walk up to the subject: the innermost enclosing named field
		// or list element. The declaration at the cursor may be a
		// sub-expression of a field's value (e.g. the 5 in `x: -5`);
		// such declarations have anonymous nodes.
		subject := subjectOf(d.Node())

		if subject != nil {
			// Walk down from the subject's declaration that contains
			// the cursor, classifying the syntax in between: crossing
			// a barrier construct (such as into a call's arguments)
			// means the cursor's position is not unified with the
			// subject's other declarations.
			barrier := false
			if decl := enclosingDecl(subject, tokFile, offset); decl != nil {
				_, barrier = walkDown(tokFile, offset, decl.Value())
			}
			if !barrier {
				addTargets(unifiedWith(subject)...)
				continue
			}
		}

		// There is no subject for this position (e.g. an expression
		// embedded at the top level of a file), or the cursor sits in
		// a sub-expression that is not unified with the subject. A
		// reference at the cursor still shows the value of whatever
		// the reference refers to: hovering the a in `x: f(a)` shows
		// a's value.
		if contains(tokFile, offset, d.Value()) {
			if ref, _ := walkDown(tokFile, offset, d.Value()); ref != nil {
				addTargets(d.Resolve(ref)...)
			}
		}
	}

	r := &renderer{
		seen:    make(map[*eval.Node]bool),
		tokFile: tokFile,
		offset:  offset,
	}
	return r.renderNodes(targets), false
}

// unifiedWith returns the subject together with every node that is
// unified with it: the subject's name (or index) looked up across the
// expansion of the subject's parent. This mirrors how navigation
// resolves the final element of a path — expand the inputs, then
// navigate by name — so, given:
//
//	a: b: x: int
//	c: a & {b: x: 4}
//
// the subject c.b.x yields {c.b.x, a.b.x}: expanding c.b sees through
// c's reference to a. Crucially the subject itself is never expanded:
// expansion cannot tell a reference conjunct from a disjunct branch,
// and expanding the subject would wrongly conjoin the branches of
// x: p | q.
//
// Like navigation, this is asymmetric: from the subject a.b.x, nothing
// links to c's declarations, and the result is just {a.b.x}.
func unifiedWith(subject *eval.Node) eval.NodeSet {
	parent := subject.Parent()
	if parent == nil {
		return eval.NodeSet{subject}
	}
	parents := eval.NodeSet{parent}.Expand()

	var result eval.NodeSet
	if name := subject.Name(); name != "" {
		result = parents.Field(name)
	} else if idx, isElem := subject.Index(); isElem {
		for _, p := range parents {
			for elem := range p.ListElements() {
				if i, ok := elem.Index(); ok && i == idx {
					result = append(result, elem)
				}
			}
		}
	}
	if len(result) == 0 {
		return eval.NodeSet{subject}
	}
	return result
}

// subjectOf returns the innermost node, starting from n and walking
// up via [eval.Node.Parent], that is a named field or a list element,
// or nil if there is no such ancestor.
func subjectOf(n *eval.Node) *eval.Node {
	for ; n != nil; n = n.Parent() {
		if n.Name() != "" {
			return n
		}
		if _, isElem := n.Index(); isElem {
			return n
		}
	}
	return nil
}

// enclosingDecl returns the subject's declaration whose value
// contains the given offset, or nil. Embedded declarations
// (conjuncts, disjuncts, comprehension bodies, ...) are skipped:
// their syntax lies within a non-embedded declaration's value, and we
// want the outermost view.
func enclosingDecl(subject *eval.Node, tokFile *token.File, offset int) *eval.Decl {
	for d := range subject.Decls() {
		if d.Kind().Embedded() {
			continue
		}
		if contains(tokFile, offset, d.Value()) {
			return d
		}
	}
	return nil
}

// contains reports whether the node's source range within tokFile
// includes the given offset.
func contains(tokFile *token.File, offset int, n ast.Node) bool {
	if n == nil {
		return false
	}
	pos, end := n.Pos(), n.End()
	if !pos.IsValid() || !end.IsValid() || pos.File() != tokFile {
		return false
	}
	return token.WithinInclusive(offset, pos, end)
}

// walkDown descends from node towards the cursor offset, at each
// level following the child that contains the offset. It reports:
//
//   - ref: the innermost path expression (ident, selector, or index
//     expression) containing the offset, if any. This is a candidate
//     for reference resolution wherever it sits, even beyond a
//     barrier.
//
//   - barrier: whether the descent crossed into a construct whose
//     interior is never unified with the enclosing field's other
//     declarations: call arguments, interpolation expressions,
//     comprehension clause sources, conditions and let values, and
//     the declarations of pattern constraints and dynamic fields.
func walkDown(tokFile *token.File, offset int, node ast.Node) (ref ast.Expr, barrier bool) {
	// beyondBarrier continues the walk from n, marking the result as
	// having crossed a barrier.
	beyondBarrier := func(n ast.Node) (ast.Expr, bool) {
		ref, _ := walkDown(tokFile, offset, n)
		return ref, true
	}
	// barrierWithin continues the walk into a barrier construct's
	// sub-expression n when it contains the offset; positions in the
	// construct's remaining syntax (keywords, binding idents) denote
	// no value.
	barrierWithin := func(n ast.Node) (ast.Expr, bool) {
		if contains(tokFile, offset, n) {
			return beyondBarrier(n)
		}
		return nil, false
	}

walk:
	for {
		switch n := node.(type) {
		case *ast.Ident:
			return n, false

		case *ast.SelectorExpr:
			if contains(tokFile, offset, n.X) {
				node = n.X
				continue
			}
			// On the selector (or the dot): the whole expression is
			// the path element to resolve.
			return n, false

		case *ast.IndexExpr:
			if contains(tokFile, offset, n.X) {
				node = n.X
				continue
			}
			if _, isLit := n.Index.(*ast.BasicLit); !isLit && contains(tokFile, offset, n.Index) {
				// A non-literal index is resolved as a nested path of
				// its own, independent of the indexing expression.
				node = n.Index
				continue
			}
			return n, false

		case *ast.BinaryExpr:
			for _, operand := range []ast.Expr{n.X, n.Y} {
				if contains(tokFile, offset, operand) {
					node = operand
					continue walk
				}
			}
			return nil, false

		case *ast.UnaryExpr:
			if contains(tokFile, offset, n.X) {
				node = n.X
				continue
			}
			return nil, false

		case *ast.ParenExpr:
			if contains(tokFile, offset, n.X) {
				node = n.X
				continue
			}
			return nil, false

		case *ast.CallExpr:
			if n.Lparen.IsValid() && offset > n.Lparen.Offset() {
				for _, arg := range n.Args {
					if contains(tokFile, offset, arg) {
						return beyondBarrier(arg)
					}
				}
				return nil, true
			}
			if contains(tokFile, offset, n.Fun) {
				node = n.Fun
				continue
			}
			return nil, false

		case *ast.Interpolation:
			for _, elt := range n.Elts {
				if _, isLit := elt.(*ast.BasicLit); isLit {
					// A literal segment of the interpolation: the
					// interpolation as a whole is the value here.
					continue
				}
				if contains(tokFile, offset, elt) {
					return beyondBarrier(elt)
				}
			}
			return nil, false

		case *ast.StructLit:
			for _, elt := range n.Elts {
				if contains(tokFile, offset, elt) {
					node = elt
					continue walk
				}
			}
			return nil, false

		case *ast.ListLit:
			for _, elt := range n.Elts {
				if contains(tokFile, offset, elt) {
					node = elt
					continue walk
				}
			}
			return nil, false

		case *ast.EmbedDecl:
			node = n.Expr
			continue

		case *ast.Alias:
			if contains(tokFile, offset, n.Expr) {
				node = n.Expr
				continue
			}
			return nil, false

		case *ast.Field:
			// Only fields whose values have anonymous nodes are
			// reachable here — pattern constraints and dynamic fields
			// — because an ordinary field's interior offsets are
			// claimed by that field's own (deeper) declarations
			// before this walk begins. Their declarations are not
			// unified with the enclosing subject's.
			if contains(tokFile, offset, n.Value) {
				return beyondBarrier(n.Value)
			}
			if contains(tokFile, offset, n.Label) {
				return beyondBarrier(n.Label)
			}
			return nil, true

		case *ast.Comprehension:
			for _, clause := range n.Clauses {
				if contains(tokFile, offset, clause) {
					node = clause
					continue walk
				}
			}
			if contains(tokFile, offset, n.Value) {
				node = n.Value
				continue
			}
			if n.Fallback != nil && contains(tokFile, offset, n.Fallback.Body) {
				node = n.Fallback.Body
				continue
			}
			return nil, false

		case *ast.ForClause:
			return barrierWithin(n.Source)

		case *ast.IfClause:
			return barrierWithin(n.Condition)

		case *ast.LetClause:
			return barrierWithin(n.Expr)

		case *ast.TryClause:
			return barrierWithin(n.Expr)

		case *ast.Ellipsis:
			if contains(tokFile, offset, n.Type) {
				node = n.Type
				continue
			}
			return nil, false

		default:
			// BasicLit, BadExpr, BottomLit, attributes, ...
			return nil, false
		}
	}
}

// renderer builds the synthetic unified-value expression. The seen
// set holds the nodes currently being rendered on this path, guarding
// against reference cycles. tokFile and offset identify the cursor:
// conjuncts from the declaration containing the cursor are rendered
// last, since the user can already see that declaration. nodeCount
// tracks how many AST nodes have been constructed, against
// [nodeBudget], and depth tracks the current output nesting depth,
// against [maxInlineDepth].
type renderer struct {
	seen      map[*eval.Node]bool
	tokFile   *token.File
	offset    int
	nodeCount int
	depth     int
}

// countNode records the construction of one AST node, abandoning the
// rendering (see [budgetExceeded]) once [nodeBudget] is exceeded.
func (r *renderer) countNode() {
	r.nodeCount++
	if r.nodeCount > nodeBudget {
		panic(budgetExceeded{})
	}
}

// renderNodes returns the conjunction of the renderings of the given
// nodes, or nil if none of them renders to anything. All of the given
// nodes count as "being rendered" for the whole call: a reference
// from one member to another resolves to a node whose rendering is
// already a conjunct, so inlining it too would only duplicate.
func (r *renderer) renderNodes(ns eval.NodeSet) ast.Expr {
	var added []*eval.Node
	for _, n := range ns {
		if !r.seen[n] {
			r.seen[n] = true
			added = append(added, n)
		}
	}
	defer func() {
		for _, n := range added {
			delete(r.seen, n)
		}
	}()

	var conjuncts []ast.Expr
	for _, n := range sortedByPosition(ns) {
		conjuncts = append(conjuncts, r.renderNode(n)...)
	}
	return r.conjoin(conjuncts)
}

// comparePositions orders source positions by filename, then by byte
// offset within the file.
func comparePositions(a, b token.Position) int {
	if c := cmp.Compare(a.Filename, b.Filename); c != 0 {
		return c
	}
	return cmp.Compare(a.Offset, b.Offset)
}

// sortedByPosition returns the nodes ordered by the source position
// of their earliest declaration: the order of an [eval.NodeSet] is
// unspecified, and rendering must both be deterministic and follow
// source order.
func sortedByPosition(ns eval.NodeSet) eval.NodeSet {
	if len(ns) < 2 {
		return ns
	}
	pos := func(n *eval.Node) (earliest token.Position) {
		first := true
		for d := range n.Decls() {
			if p := declPos(d); first || comparePositions(p, earliest) < 0 {
				earliest, first = p, false
			}
		}
		return earliest
	}
	ns = slices.Clone(ns)
	slices.SortStableFunc(ns, func(a, b *eval.Node) int {
		return comparePositions(pos(a), pos(b))
	})
	return ns
}

// declPos returns the source location of a declaration: the position
// of its key, or failing that of its value.
func declPos(d *eval.Decl) token.Position {
	for _, node := range []ast.Node{d.Key(), d.Value()} {
		if node == nil {
			continue
		}
		if p := node.Pos(); p.IsValid() {
			return p.Position()
		}
	}
	return token.Position{}
}

// renderNode returns one conjunct per renderable declaration of n, in
// source order (a node's declarations are ordered by discovery, which
// can differ from source order, e.g. for a field declared within a
// comprehension body) — except that the declaration containing the
// cursor comes last: the user can already see it, making it the least
// informative part of the result.
func (r *renderer) renderNode(n *eval.Node) []ast.Expr {
	var decls []*eval.Decl
	for d := range n.Decls() {
		switch d.Kind() {
		case eval.DeclField, eval.DeclAlias, eval.DeclPattern, eval.DeclDynamic:
		default:
			// Embedded declarations (conjuncts, disjuncts, defaults,
			// comprehension bodies) are parts of a DeclField's value
			// and are rendered within it; files, packages, imports
			// and inline expressions carry no value of their own.
			continue
		}
		value, ok := d.Value().(ast.Expr)
		if !ok {
			continue
		}
		if _, isBad := value.(*ast.BadExpr); isBad {
			// An incomplete declaration, e.g. `x: ` with no value
			// yet, contributes nothing.
			continue
		}
		decls = append(decls, d)
	}
	slices.SortStableFunc(decls, func(a, b *eval.Decl) int {
		if aCursor, bCursor := r.declContainsCursor(a), r.declContainsCursor(b); aCursor != bCursor {
			if aCursor {
				return 1
			}
			return -1
		}
		return comparePositions(declPos(a), declPos(b))
	})

	conjuncts := make([]ast.Expr, len(decls))
	for i, d := range decls {
		conjuncts[i] = r.inlineExpr(d, d.Value().(ast.Expr))
	}
	return conjuncts
}

// declContainsCursor reports whether the declaration's source extent
// — from its key (or value, if it has no key) to the end of its value
// — contains the renderer's cursor. The declaration must have a value
// ([renderNode] renders no others).
func (r *renderer) declContainsCursor(d *eval.Decl) bool {
	start := d.Key()
	if start == nil {
		start = d.Value()
	}
	startPos, endPos := start.Pos(), d.Value().End()
	if !startPos.IsValid() || !endPos.IsValid() || startPos.File() != r.tokFile {
		return false
	}
	return token.WithinInclusive(r.offset, startPos, endPos)
}

// inlineExpr returns a copy of expr — a value of the declaration d —
// with every reference throughout it replaced by the rendering of
// what the reference refers to, recursively.
func (r *renderer) inlineExpr(d *eval.Decl, expr ast.Expr) ast.Expr {
	if replacement, ok := r.inlineReference(d, expr); ok {
		return replacement
	}
	c := copier{r: r, d: d}
	return c.node(expr).(ast.Expr)
}

// inlineReference renders the targets that the reference expression
// ref (an element of d's declaration's syntax) refers to. It reports
// false — no replacement — when ref sits too deep in the output (see
// [maxInlineDepth]), is not a reference expression at all, does not
// resolve, resolves only to targets already being rendered on this
// path (a cycle), or its targets render to nothing; in each case the
// caller keeps the reference as written.
func (r *renderer) inlineReference(d *eval.Decl, ref ast.Expr) (ast.Expr, bool) {
	if r.depth > maxInlineDepth {
		return nil, false
	}
	switch n := ref.(type) {
	case *ast.Ident, *ast.SelectorExpr:
	case *ast.IndexExpr:
		if _, isLit := n.Index.(*ast.BasicLit); !isLit {
			// A non-literal index resolves to the definition of the
			// index expression itself, not to an element of the
			// indexed value, so only literal indices are inlined.
			return nil, false
		}
	default:
		return nil, false
	}

	var targets eval.NodeSet
	for _, target := range d.Resolve(ref) {
		if !r.seen[target] {
			targets = append(targets, target)
		}
	}
	rendered := r.renderNodes(targets)
	if rendered == nil {
		return nil, false
	}
	return rendered, true
}

// conjoin joins the conjuncts with &, or returns the sole conjunct
// unchanged. Conjuncts that are themselves conjunctions are flattened
// into the chain (the joined tree is left-nested; a nested & subtree
// on the right would print parenthesized). Disjunctions are
// parenthesized: | binds looser than &. Every other binary operator
// binds tighter than & and needs no parentheses.
func (r *renderer) conjoin(conjuncts []ast.Expr) ast.Expr {
	var flat []ast.Expr
	var flatten func(c ast.Expr)
	flatten = func(c ast.Expr) {
		if bin, isBin := c.(*ast.BinaryExpr); isBin && bin.Op == token.AND {
			flatten(bin.X)
			flatten(bin.Y)
			return
		}
		flat = append(flat, c)
	}
	for _, c := range conjuncts {
		flatten(c)
	}

	if len(flat) == 1 {
		return flat[0]
	}
	var result ast.Expr
	for _, c := range flat {
		if bin, isBin := c.(*ast.BinaryExpr); isBin && bin.Op == token.OR {
			r.countNode()
			c = &ast.ParenExpr{X: c}
		}
		if result == nil {
			result = c
		} else {
			r.countNode()
			result = &ast.BinaryExpr{X: result, Op: token.AND, Y: c}
		}
	}
	return result
}
