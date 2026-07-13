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

// WARNING: THIS PACKAGE IS EXPERIMENTAL.
// ITS API MAY CHANGE AT ANY TIME.
//
// # The Graph API
//
// This evaluator also supports a graph API: a structured, navigable
// view of the graph that the evaluator lazily constructs. Exploring
// the graph provokes any necessary evaluation (call-by-need). The
// graph API has three layers:
//
//  1. [Node]: the semantic layer. A Node is a vertex of the graph,
//     presenting the merged view of all the declarations that jointly
//     define one thing. Fields are the most common Nodes
//     (declarations of the same field name under the same parent Node
//     all contribute to a single Node, however far apart they are
//     lexically) but not the only ones: the package root, list
//     elements, ellipses, pattern constraints and dynamic fields,
//     alias and let bindings, and inline expressions all have Nodes
//     too, most of them anonymous. Nodes are canonical: the same
//     *Node always represents the same vertex, however it is
//     discovered.
//
//  2. [Decl]: the syntactic layer. Each Node aggregates one or more
//     declarations; each is exposed as a Decl retaining its key, its
//     value expression, its doc comments, and how it contributes to
//     its node ([Decl.Kind]). The Decl's value is a plain [ast.Node]:
//     callers walk it themselves, and can hand any element they find
//     back to [Decl.Resolve] to get at the [Node]s that element
//     refers to.
//
//  3. [NodeSet]: a set of Nodes. Several questions are legitimately
//     answered by more than one node; a NodeSet's methods ask the
//     same question of every member and merges the
//     results. [Node.Expand] returns everything a node transitively
//     includes by resolution. For example, given:
//
//     x: {a: 1}
//     x: y
//     y: {b: 2}
//
//     x.Expand() is the set {x, y}, and x.Expand().Fields() is the
//     complete merged view of x's fields - a and b. [Decl.Resolve]
//     also returns a set, because an expression can resolve to
//     several declarations. For example, given:
//
//     y: a: 1
//     z: a: 2
//     x: y
//     x: z
//     w: x.a
//
//     the ident `a` in w's value resolves to two nodes - y's `a` and
//     z's `a` - because navigating `x.a` passes through both of x's
//     references.
//
// [Node.Fields] is purely syntactic containment, yielding the fields
// whose declarations are literally present within the node's own
// declarations, including conjunction and disjunction operands,
// embedded struct literals, and comprehension bodies; whereas
// [Node.Expand] follows resolution: references, embedded selector
// expressions, and imports. For example, given:
//
//	x: {a: 1} & {b: 2}
//	x: {c: 3, if p {d: 4}}
//	x: e
//	e: f: 5
//	p: true
//
// x's Fields are `a` and `b` (conjunction operands), `c` (a plain
// field), and d (a comprehension body). `f` is not among them: it is
// only reachable by resolving the reference `e`, and so it appears
// only in `x.Expand().Fields()`. The same distinction occurs at the
// [Decl] layer: [Decl.Fields] recovers the per-declaration grouping
// of the syntactic side, while [Decl.Resolve] provides the
// syntax-anchored edges of the resolution side.
package eval

import (
	"cmp"
	"fmt"
	"iter"
	"maps"
	"slices"
	"strconv"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

// A Node is a vertex of the evaluator's graph: the merged view of all
// the declarations that jointly define one thing. Fields are the most
// common Nodes (declarations of the same field name under the same
// parent Node all contribute to a single Node, however far apart they
// are lexically) but far from the only ones: the package root, list
// elements (see [Node.Index]), ellipses (see [Node.Ellipses]),
// pattern constraints and dynamic fields (see [Node.Patterns] and
// [Node.Dynamics]), alias and let bindings, and inline expressions
// (the struct in `{a: 3}.a`) all have Nodes too, most of them
// anonymous ([Node.Name] returns ""). For example, given:
//
//	x: {y: 3}
//	x: {y: int}
//
// there is a single Node for `x`, whose [Node.Decls] yields one
// [Decl] per declaration; and there is also a single Node for `y`,
// even though its two declarations lie in different lexical scopes
// (two distinct struct literals): they declare the same name under
// the same parent node, and so they are merged.
//
// Nodes are canonical: within a single [Evaluator] (between calls to
// [Evaluator.Reset]), the same vertex is always represented by the
// same *Node, no matter how it is discovered. For example, given:
//
//	x: y: 5
//	z: x
//
// the Node for `x.y` obtained via `Root().Field("x").Field("y")` is
// the same *Node as the one found by expanding `z` (see
// [Node.Expand]) and looking up `y`.
//
// Obtain the root Node with [Evaluator.Root], descend into fields
// with [Node.Fields] or [Node.Field], follow resolution with
// [Node.Expand], and recover the individual declarations that
// contribute to a Node with [Node.Decls].
type Node navigable

// Root returns the [Node] for the package's top-level scope. Its
// fields are the top-level fields declared across all of the
// package's files, and its [Node.Decls] yields one [Decl] of kind
// [DeclFile] per file, plus a Decl for anything embedded at the top
// level of a file. For example, given:
//
//	-- a.cue --
//	package p
//	x: 3
//	-- b.cue --
//	package p
//	y
//	y: 4
//
// the root node has fields x and y, and three Decls: two of kind
// [DeclFile] (one per file) and one of kind [DeclEmbedding] (the
// embedded y in b.cue). The field decls are available via
// `Root().Field("x").Decls()`, for example.
//
// The package clauses are not part of the root node: see
// [Evaluator.PackageClauses].
func (e *Evaluator) Root() *Node {
	e.bootFiles()
	return (*Node)(e.fileFramesNav)
}

// PackageClauses returns the [Node] that aggregates the package
// clauses across the package's files. Its [Node.Decls] yields one
// [Decl] of kind [DeclPackage] per package clause; the doc comments
// of those Decls are the package documentation. For example, given:
//
//	// Package p frobnicates.
//	package p
//
// the returned node has one Decl whose [Decl.Key] is the ident p and
// whose [Decl.DocComments] contains the comment.
//
// The returned node has no fields of its own: navigation into the
// contents of a package goes via [Evaluator.Root] (or, from an
// importing package, via [Node.Expand] on the node that an import's
// ident resolves to). Resolving an [ast.ImportSpec] with
// [Decl.Resolve] yields the imported package's PackageClauses node.
func (e *Evaluator) PackageClauses() *Node {
	e.bootFiles()
	return (*Node)(e.pkgDecls)
}

// Evaluator returns the [Evaluator] of the package to which this node
// belongs. Nodes reached through imports (via [Node.Expand] or
// [Decl.Resolve]) belong to the imported package's evaluator.
func (n *Node) Evaluator() *Evaluator {
	return n.evaluator
}

// Name returns the field name of this node, or "" if this node does
// not correspond to a named field. For example, given:
//
//	x: y: 3
//
// the node for x.y has name "y". Anonymous nodes include the package
// root, list elements (see [Node.Index]), ellipses (see
// [Node.Ellipses]), pattern constraints and dynamic fields (see
// [Node.Patterns] and [Node.Dynamics]), and alias and let bindings.
func (n *Node) Name() string {
	name := n.name
	if strings.HasPrefix(name, "__") {
		return ""
	}
	return name
}

// Index returns the index of this node within its parent list, for
// nodes that are list elements. For example, given:
//
//	l: [7, 8]
//
// the node for the 8 has index 1. The second return value reports
// whether this node is a list element.
//
// Indices are purely syntactic: each element expression of a list
// literal gets one index, in source order. In particular, a
// comprehension within a list occupies a single index no matter how
// many elements it would yield at runtime: this evaluator does not
// model the dynamically generated elements. For example, given:
//
//	l: [m, for x in [2, 3] {x}, n]
//
// `l` has three element nodes: `m` at index 0, the comprehension at
// index 1, and the `n at index 2 (even though full evaluation would
// place it at index 3).
//
// Aside: to access this comprehension itself, use the element node's
// [Decl]s: its Decl of kind [DeclField] holds the whole comprehension
// (clauses and all) as its Value, while its Decl of kind
// [DeclComprehension] holds just the comprehension's body (the {x}
// above).
func (n *Node) Index() (int, bool) {
	rest, found := strings.CutPrefix(n.name, "__")
	if !found {
		return 0, false
	}
	i, err := strconv.Atoi(rest)
	if err != nil || i < 0 {
		return 0, false
	}
	return i, true
}

// Constraint reports the effective field constraint of this node: the
// unification, per [UnifyConstraints], of the constraints of its
// field declarations. Declarations of other kinds (conjunction and
// disjunction operands, comprehension bodies, embeddings etc) carry
// no constraint and take no part, so, given:
//
//	x?: {a: 1} & {b: 2}
//	x!: 2
//
// x's Constraint is [ConstraintRequired]: the conjunction operands do
// not make x regular, and the required declaration wins over the
// optional one.
//
// The second return value reports whether the node has any field
// declarations at all: anonymous nodes such as pattern constraints,
// dynamic fields, or let bindings have none, and their lack of a
// constraint is distinct from a regular field's [ConstraintRegular].
//
// The constraint is a property of the node's own declarations, not of
// anything reached by resolution: in
//
//	x: _
//	y?: x
//
// y's Constraint is [ConstraintOptional]; referencing the regular
// field x does not affect y's optionality. For the same reason, take
// care when combining the Constraints of several nodes: unifying them
// with [UnifyConstraints] is meaningful only for nodes that are
// same-named fields merged by struct unification, not for a node and
// its reference targets (e.g. the members of [Node.Expand]).
func (n *Node) Constraint() (Constraint, bool) {
	// The fold starts from optional, the weakest constraint in the
	// subsumption order.
	constraint := ConstraintOptional
	fieldDecls := false
	for d := range n.Decls() {
		if d.Kind() != DeclField {
			continue
		}
		fieldDecls = true
		constraint = UnifyConstraints(constraint, d.Constraint())
	}
	if !fieldDecls {
		return ConstraintRegular, false
	}
	return constraint, true
}

// Parent returns the node within which this node is declared, or nil
// for the package root (and for internal nodes that hang directly off
// the package scope, such as [Evaluator.PackageClauses]). The parent
// is determined by declaration, not by navigation: in
//
//	x: a: 3
//	y: x
//
// the node for a is found both under x and by expanding y, but its
// Parent is always the node for x.
func (n *Node) Parent() *Node {
	nav := (*navigable)(n)
	parent := nav.parent
	if parent == nil || parent == nav.evaluator.pkgFrame.navigable {
		return nil
	}
	return (*Node)(parent)
}

// FieldPath returns the sequence of field names leading from the
// package root to this node, and reports whether such a path exists.
// The package root itself has an empty path. For example, given:
//
//	x: y: z: 3
//
// the node for z has path ["x", "y", "z"].
//
// The path is deliberately field names only. A field's name is an
// intrinsic property of its declaration: struct membership is by
// name: nothing elsewhere in the program can rename a field; and
// names are only modeled at all when they are statically known.
// Consequently a returned path is guaranteed to be the true location
// of this node's declarations: FieldPath never mislocates. By
// contrast, a list element's index is contextual: a comprehension
// earlier in the list shifts, at runtime, the position of the
// elements that follow it (see [Node.Index]), so list indices could
// be wrong, and so are excluded.
//
// However, it is not guaranteed is that the path is inhabited after
// full evaluation. This evaluator is a MAY-analysis (see the package
// documentation): the node may originate from a disjunction branch
// that full evaluation discards, or a comprehension body whose
// condition is false, in which case nothing exists at the path in the
// final result.
//
// Not every node is addressable by a path of names: list elements,
// ellipses, pattern constraints and dynamic fields, alias and let
// bindings, and inline expressions (e.g. the struct in {a: 3}.a) have
// no path, as does any node whose ancestry passes through one of
// these nodes. In these cases FieldPath returns nil, false.
//
// A node reached through an import has a path relative to the root of
// its own package: use [Node.Evaluator] to detect that a node belongs
// to a different package.
func (n *Node) FieldPath() ([]string, bool) {
	var names []string
	for nav := (*navigable)(n); ; nav = nav.parent {
		if nav == nav.evaluator.fileFramesNav {
			slices.Reverse(names)
			return names, true
		}
		if nav.parent == nil || nav.name == "" || strings.HasPrefix(nav.name, "__") {
			return nil, false
		}
		names = append(names, nav.name)
	}
}

// Fields yields this node's own fields, in lexical order of name: a
// field is yielded here iff it is declared by a field declaration
// syntactically contained within one of this node's declarations.
// Containment sees through constructs that are literally present in
// the source (conjunction and disjunction operands, embedded struct
// literals, comprehension bodies, defaults, and duplicate
// declarations of the same field) but never through a reference:
// fields that are only reachable by resolving an embedded ident,
// selector, index expression, or an import, are not included. Fields
// reached via references are available through [Node.Expand]. For
// example, given:
//
//	x: {a: 1} & {b: 2}
//	x: {c: 3, d}
//	d: e: 4
//
// x's Fields are a, b and c. e is not among them, because it is only
// reached by resolving the embedded reference d; it appears via
// x.Expand().Fields(). Consequently, every field yielded here has at
// least one [Decl] whose Key lies within the source range of one of
// this node's own declarations.
//
// The names yielded are the string values of the fields' labels,
// never their source syntax: a quoted label names the same field as
// an ident label spelling the same string, and so is never yielded
// quoted - even when the name is not a valid identifier ("a b": 1
// declares a field yielded under the name `a b`). For example, given:
//
//	x: y: 1
//	x: "y": int
//
// x has a single field, yielded under the name `y`, whose node merges
// both declarations. To recover how each declaration spelled its
// label, use [Decl.Label], which returns the original syntax (the
// [ast.Ident] and the [ast.BasicLit] here, respectively).
//
// Fields does not distinguish between the branches of a disjunction:
//
//	x: {a: 1} | {b: 2}
//
// x's Fields are a and b (this evaluator is a MAY-analysis: see the
// package documentation). Use [Node.Decls] and [Decl.Fields] to
// recover the per-branch grouping.
//
// Several forms of field declaration never appear as fields of any
// node:
//
//   - pattern constraints: [string]: a: 3
//   - dynamic fields: (m): 3, "\(m)": 3
//
// The evaluator tracks their declarations (see [Node.Patterns] and
// [Node.Dynamics]) but never matches field names against patterns,
// nor computes the names of dynamic fields.
//
// Additionally, a field's optionality does not affect the model: `x?:
// 3` and `x!: 3` declare a field `x` exactly as `x: 3` does. The
// optionality marker is reported by [Decl.Constraint].
//
// Lexical-only bindings are not fields, and are never yielded here
// nor found by [Node.Field]: let clauses, field aliases (X=y: 5
// declares only the field y; X binds y's value), and the names bound
// by imports all introduce names that paths within their scope can
// use, but declare no field. Such uses still resolve as normal via
// [Decl.Resolve]: a use of the X above yields y's node, and a use of
// a let's name yields the let's own anonymous node.
//
// List elements are not yielded here: see [Node.ListElements].
func (n *Node) Fields() iter.Seq2[string, *Node] {
	return func(yield func(string, *Node) bool) {
		nav := (*navigable)(n)
		nav.eval()
		for _, name := range slices.Sorted(maps.Keys(nav.bindings)) {
			if strings.HasPrefix(name, "__") {
				continue
			}
			if !yield(name, (*Node)(nav.bindings[name])) {
				return
			}
		}
	}
}

// Field returns the node for the named field, or nil if this node has
// no such field. The same containment rules apply as for
// [Node.Fields]: a field that is only reachable via a reference is
// not found. For the merged view, expand first:
//
//	x: a: 3
//	y: x
//
// y.Field("a") is nil, whereas y.Expand().Field("a") finds a.
//
// The name to pass is the label's string value, exactly as yielded by
// [Node.Fields] - never quoted, even when the name is not a valid
// identifier: the field "a b": 1 is found by Field("a b"), and
// Field(`"a b"`) finds nothing. Lexical-only bindings (lets, field
// aliases, the names bound by imports) are not fields, and are never
// found here.
func (n *Node) Field(name string) *Node {
	if strings.HasPrefix(name, "__") {
		return nil
	}
	nav := (*navigable)(n)
	nav.eval()
	childNav, found := nav.bindings[name]
	if !found {
		return nil
	}
	return (*Node)(childNav)
}

// ListElements yields the nodes for this node's list elements, in
// index order. As with fields, the elements of all of this node's
// declarations are merged, positionally. For example, given:
//
//	l: [7]
//	l: [8, 9]
//
// l has two elements: the first aggregates the declarations of 7 and
// 8 (its node has two Decls), and the second is the 9. Element nodes
// are anonymous ([Node.Name] returns ""); their position is reported
// by [Node.Index].
//
// Whether a list is open is not part of its elements: see
// [Node.Ellipses].
func (n *Node) ListElements() iter.Seq[*Node] {
	return func(yield func(*Node) bool) {
		(*navigable)(n).eval()
		type element struct {
			index int
			nav   *navigable
		}
		var elements []element
		for name, nav := range n.bindings {
			rest, found := strings.CutPrefix(name, "__")
			if !found {
				continue
			}
			i, err := strconv.Atoi(rest)
			if err != nil || i < 0 {
				continue
			}
			elements = append(elements, element{index: i, nav: nav})
		}
		slices.SortFunc(elements, func(a, b element) int {
			return cmp.Compare(a.index, b.index)
		})
		for _, element := range elements {
			if !yield((*Node)(element.nav)) {
				return
			}
		}
	}
}

// Ellipses returns the nodes for the ellipses declared within this
// node's declarations. An ellipsis gets its own anonymous node,
// whose single [Decl] has kind [DeclEllipsis]: that
// Decl's Key is the [ast.Ellipsis] itself, and its Value is the
// ellipsis's type expression, if any. For example, given:
//
//	l: [1, ...int]
//	x: {a: 1, ...}
//
// l and x each have one ellipsis node: l's has the ident int as its
// Decl's Value, whereas x's has no Value. Because this node's
// declarations include disjunction operands and other embedding-like
// constructs, use [Decl.Ellipses] to recover which declaration an
// ellipsis belongs to, e.g. to determine which branch of a
// disjunction is open.
func (n *Node) Ellipses() NodeSet {
	(*navigable)(n).eval()
	var result NodeSet
	for _, fr := range n.frames {
		for _, nav := range fr.ellipses {
			result = append(result, (*Node)(nav))
		}
	}
	return result
}

// Patterns returns the nodes for the pattern constraints declared
// within this node's declarations. A pattern constraint gets its own
// anonymous node, whose single [Decl] has kind [DeclPattern]: that
// Decl's Key is the pattern's bracketed label (or the value alias
// ident, if one is present), its Label is the bracketed label
// regardless of aliasing, and its Value is the pattern's value. For
// example, given:
//
//	x: {[string]: int}
//	y: {[=~"^a"]: b: 3} | {c: 4}
//
// x and y each have one pattern node. The evaluator never matches
// field names against patterns: a pattern contributes no fields to
// its enclosing node, and takes no part in navigation. Its value
// remains traversable, though: b is a field of y's pattern node.
// Because this node's declarations include disjunction operands and
// other embedding-like constructs, use [Decl.Patterns] to recover
// which declaration a pattern belongs to, e.g. to determine which
// branch of a disjunction declares it.
func (n *Node) Patterns() NodeSet {
	(*navigable)(n).eval()
	var result NodeSet
	for _, fr := range (*navigable)(n).frames {
		for _, nav := range fr.patterns {
			result = append(result, (*Node)(nav))
		}
	}
	return result
}

// Dynamics returns the nodes for the dynamic fields (fields whose
// names are computed by an expression or an interpolation) declared
// within this node's declarations. A dynamic field gets its own
// anonymous node, whose single [Decl] has kind [DeclDynamic]: that
// Decl's Key is the field's label (or the value alias ident, if one
// is present), its Label is the field's label regardless of aliasing,
// and its Value is the field's value. For example, given:
//
//	k: "kk"
//	l=(k): {a: 1}
//	"\(k)-b": c: 2
//	use: l.a
//
// the root node has two dynamic nodes. The first's Decl has the alias
// ident l as its Key and the parenthesized (k) as its Label; the
// second's Key and Label are both the interpolation.
//
// The evaluator never computes the names of dynamic fields, even when
// it is trivial to do so: a dynamic field contributes no fields to
// its enclosing node, and cannot be reached by navigating by
// name. Its value remains traversable, though: c is a field of the
// second dynamic node, and a value alias, being an ordinary lexical
// binding, is the one route by which paths reach into a dynamic
// field's value: the `l.a` in use's value resolves to the `a` of the
// first dynamic node. Use [Decl.Dynamics] to recover which
// declaration a dynamic field belongs to.
func (n *Node) Dynamics() NodeSet {
	(*navigable)(n).eval()
	var result NodeSet
	for _, fr := range (*navigable)(n).frames {
		for _, nav := range fr.dynamics {
			result = append(result, (*Node)(nav))
		}
	}
	return result
}

// Expand returns this node together with every node transitively
// reachable from it by resolution: the nodes whose contents this node
// includes. It is a convenience method: it does nothing more than
// `NodeSet{n}.Expand()`
//
// Resolution edges arise from embedded references and reference
// values:
//
//	x: y            // x's Node expands to the NodeSet {x, y}
//	z: {y, a: 1}    // z's Node expands to the NodeSet {z, y}
//
// as well as from aliases, imports, and from inclusions implied by
// unification. As an example of the latter:
//
//	a: b: c: 3
//	x: a
//	x: b: d: 4
//
// x.b includes a.b even though no expression says so directly, so
// x.b's Node expands to {x.b, a.b}. Such implied edges mean that
// expansion can discover strictly more than walking the declarations'
// syntax and resolving what you find.
//
// The union of the Fields of the returned set is the complete merged
// view of this node:
//
//	x: {a: 1}
//	x: y
//	y: {b: 2}
//
// x.Fields yields only a, whereas x.Expand().Fields() yields a and
// b. The returned set always includes the receiver.
//
// Because this evaluator is a MAY-analysis (see the package
// documentation), expansion follows all branches of disjunctions, and
// may include nodes that full evaluation would prove unreachable. For
// example, given:
//
//	x: y | z
//	y: a: 1
//	z: b: 2
//
// x expands to {x, y, z}, and x.Expand().Fields() yields both a and
// b. Comprehensions are treated the same way: bodies are always
// followed, and guards are never evaluated. Given:
//
//	something: bool
//	x: {if something {y}}
//	y: b: 2
//
// x expands to {x, y} whether or not `something` is true. The same
// holds one layer down, for fields declared within a comprehension
// body: in
//
//	x: {if something {y?: bool}}
//
// y is a field of x's own node (see [Node.Fields]) regardless of
// the guard, with no expansion involved.
func (n *Node) Expand() NodeSet {
	return NodeSet{n}.Expand()
}

// Decls returns an iterator over the distinct declarations that
// contribute to this node. A node may be defined by several
// declarations, and each is yielded as a separate [Decl]. This
// includes declarations that are merged into the node by
// embedding-like constructs. For example, given:
//
//	x: {a: 1} & {b: 2}
//
// x's node has three Decls: the field declaration itself (kind
// [DeclField], whose Value is the whole binary expression), and one
// Decl of kind [DeclConjunct] per operand (whose Values are the
// operands). All three return x's node from [Decl.Node]: use
// [Decl.Kind], [Decl.Value] and [Decl.Fields] to tell them apart.
func (n *Node) Decls() iter.Seq[*Decl] {
	return func(yield func(*Decl) bool) {
		nav := (*navigable)(n)
		nav.eval()
		for _, fr := range nav.frames {
			if !yield((*Decl)(fr)) {
				return
			}
		}
	}
}

// A NodeSet is a set of [Node]s: each node appears at most once, and
// only membership is meaningful - the order of the members is
// unspecified, and must not be relied upon. NodeSets arise wherever
// several nodes can legitimately answer a question: most prominently
// from [Node.Expand], where the set as a whole represents the merged
// view of a node, and from [Decl.Resolve], where an expression can
// resolve to several declarations. The methods on NodeSet ask the
// same question of every member and merge the answers.
//
// Whether a NodeSet has been expanded is a property of how it was
// produced: no method expands implicitly, because expansion changes
// what the set means (see [Node.Expand]). In particular,
// [Decl.Resolve] deliberately returns an unexpanded set. For example,
// given:
//
//	x: {a: 1}
//	y: x
//	z: y
//
// resolving the ident y in z's value yields the set {y}. Unexpanded,
// it answers questions about y itself: its Decls are the declarations
// of y alone: the definition sites, which is what jump-to-definition
// wants for example:
//
//	resolved.Decls()           // the declaration y: x
//
// Expanding first sees through the reference, so the set answers
// questions about everything y includes (transitively): the merged
// view, which is what completion wants for example:
//
//	resolved.Expand().Fields() // the field a, via x
type NodeSet []*Node

// dedupeNodes returns ns with duplicate members removed, keeping the
// first occurrence of each. Returns nil if the result would be empty.
func dedupeNodes(ns NodeSet) NodeSet {
	seen := make(map[*Node]struct{}, len(ns))
	var result NodeSet
	for _, n := range ns {
		if _, found := seen[n]; found {
			continue
		}
		seen[n] = struct{}{}
		result = append(result, n)
	}
	return result
}

// Expand returns the union of the members' [Node.Expand] sets.
func (ns NodeSet) Expand() NodeSet {
	seen := make(map[*Node]struct{}, len(ns))
	worklist := slices.Clone(ns)
	for len(worklist) > 0 {
		n := worklist[0]
		worklist = worklist[1:]
		if _, found := seen[n]; found {
			continue
		}
		seen[n] = struct{}{}

		nav := (*navigable)(n)
		nav.eval()
		for target := range nav.resolvesTo {
			worklist = append(worklist, (*Node)(target))
		}
	}
	return slices.Collect(maps.Keys(seen))
}

// Fields yields the members' own fields (see [Node.Fields]), grouped
// by name, in lexical order of name. Each yielded NodeSet holds the
// nodes for one name. The NodeSet ns is not implicitly expanded, nor
// are the yielded NodeSets.
//
// The member sets preserve provenance. For example, given:
//
//	x: {a: 1}
//	x: y
//	y: {a: 2}
//
// x.Expand().Fields() yields the single name a, whose NodeSet has two
// members: the node for x's own a, and the node for y's a.
// [Node.Parent] reveals which is which: the member declared directly
// below x has Parent == x's node.
func (ns NodeSet) Fields() iter.Seq2[string, NodeSet] {
	return func(yield func(string, NodeSet) bool) {
		var names []string
		byName := make(map[string]NodeSet)
		for _, n := range ns {
			for name, child := range n.Fields() {
				if _, found := byName[name]; !found {
					names = append(names, name)
				}
				byName[name] = append(byName[name], child)
			}
		}
		slices.Sort(names)
		for _, name := range names {
			if !yield(name, dedupeNodes(byName[name])) {
				return
			}
		}
	}
}

// Field returns the nodes for the named field across the members of
// ns (see [Node.Field]). The NodeSet ns is not implicitly expanded,
// nor is the result: n.Expand().Field("a") finds every declaration
// site of the field `a` within the merged view of n, whereas
// NodeSet{n}.Field("a") only consults n's own declarations.
func (ns NodeSet) Field(name string) NodeSet {
	var result NodeSet
	for _, n := range ns {
		if child := n.Field(name); child != nil {
			result = append(result, child)
		}
	}
	return dedupeNodes(result)
}

// Decls yields the declarations of every member of the set (see
// [Node.Decls]). The NodeSet ns is not implicitly expanded.
func (ns NodeSet) Decls() iter.Seq[*Decl] {
	return func(yield func(*Decl) bool) {
		seen := make(map[*Decl]struct{})
		for _, n := range ns {
			for d := range n.Decls() {
				if _, found := seen[d]; found {
					continue
				}
				seen[d] = struct{}{}
				if !yield(d) {
					return
				}
			}
		}
	}
}

// DeclKind classifies how a [Decl] contributes to its [Node].
type DeclKind int

const (
	// DeclExpression is a declaration that does not correspond to any
	// source-level declaration of its node: an inline expression that
	// the evaluator tracks for resolution purposes, such as the
	// argument of a function call.
	DeclExpression DeclKind = iota

	// DeclFile is a file's contribution to the package root: the
	// Decl's Value is the [ast.File], and its Key is nil. See
	// [Evaluator.Root].
	DeclFile

	// DeclPackage is a package clause: the Decl's Key is the package
	// name ident, and its doc comments are the package
	// documentation. See [Evaluator.PackageClauses].
	DeclPackage

	// DeclImport is an import spec: the Decl's Value is the
	// [ast.ImportSpec], and its Key is the alias ident, if present, or
	// else the import path. The Decl's node is the binding the import
	// establishes in its file: expanding that node (see [Node.Expand])
	// reaches the imported package's root.
	DeclImport

	// DeclField is an ordinary field declaration:
	//
	//	key: value
	//
	// This includes optional (key?: value) and required (key!: value)
	// fields (whose optionality is reported by [Decl.Constraint] but
	// does not affect the model) and list elements.
	DeclField

	// DeclPattern is a pattern constraint:
	//
	//	[string]: {a: 1}
	//
	// The Decl's Value is the pattern's value; its Key is the
	// bracketed label (an [ast.ListLit]), or the value alias ident
	// when one is present (v=[string]: ...). The evaluator never
	// matches field names against patterns: a pattern's node is
	// anonymous, contributes no fields to its enclosing node, and
	// takes no part in navigation. Its value remains traversable:
	// paths within it resolve as usual. Pattern nodes are
	// discovered via [Node.Patterns] and [Decl.Patterns].
	DeclPattern

	// DeclDynamic is a field whose name is computed by an
	// expression or an interpolation:
	//
	//	(expr): {a: 1}
	//	"\(expr)": {a: 1}
	//
	// The Decl's Value is the field's value; its Key is the label
	// (an [ast.ParenExpr] or [ast.Interpolation]), or the value
	// alias ident when one is present. The evaluator never computes
	// the field's name, even when it is trivial to do so: a dynamic
	// field's node is anonymous, contributes no fields to its
	// enclosing node, and takes no part in navigation. Its value
	// remains traversable: paths within it resolve as usual.
	// Dynamic field nodes are discovered via [Node.Dynamics] and
	// [Decl.Dynamics].
	DeclDynamic

	// DeclAlias is an alias-like lexical binding: a field alias
	// (X=key: value), a let clause, or the identifiers bound by a for
	// or try clause. Such bindings are only visible as the first
	// element of a path, so their nodes are anonymous and are only
	// discovered by resolving a use ([Decl.Resolve]).
	DeclAlias

	// DeclEmbedding is an expression embedded within a struct, or at
	// the top level of a file:
	//
	//	x: {y, a: 1}
	//
	// The embedded y is a DeclEmbedding contributing to x.
	DeclEmbedding

	// DeclConjunct is an operand of a unification expression:
	//
	//	x: {a: 1} & {b: 2}
	//
	// Each operand is a DeclConjunct contributing to x. Note that the
	// operands mirror the parsed expression tree, not the flattened
	// chain: x: a & b & c yields a DeclConjunct for the interior a & b
	// expression as well as one per operand, and a parenthesized
	// operand's Decl holds the [ast.ParenExpr]. A consumer that wants
	// the structure of an expression (its operands, grouping, or
	// nesting) should not reconstruct it from these Decls, but walk
	// the declaration's Value, which holds the authoritative syntax,
	// and use [Decl.Resolve] to get back to the [Node]s for the
	// elements it finds.
	DeclConjunct

	// DeclDisjunct is an operand of a disjunction expression:
	//
	//	x: {a: 1} | {b: 2}
	//
	// Each operand is a DeclDisjunct contributing to x. As with
	// [DeclConjunct], the operands mirror the parsed expression tree:
	// a consumer that wants the branch structure of a disjunction
	// should walk the declaration's Value rather than reconstruct it
	// from these Decls.
	DeclDisjunct

	// DeclDefault is the operand of a unary * (default) expression:
	//
	//	x: *{a: 1} | {b: 2}
	//
	// The {a: 1} contributes to x both as a DeclDefault and,
	// separately, as the DeclDisjunct for the whole *{a: 1} operand.
	DeclDefault

	// DeclComprehension is the body of an if, for, let or try
	// comprehension:
	//
	//	x: {if a > 2 {b: 3}}
	//
	// The {b: 3} is a DeclComprehension contributing to x. Note that
	// as a MAY-analysis, the evaluator includes comprehension bodies
	// unconditionally: it does not attempt to determine whether the
	// comprehension actually yields anything.
	DeclComprehension

	// DeclEllipsis is an ellipsis declaration ("..." or "...T"),
	// within either a struct or a list. See [Node.Ellipses].
	DeclEllipsis
)

// Embedded reports whether declarations of this kind are merged into
// their node by an embedding-like construct (an embedding, a
// conjunction or disjunction operand, a default, or a comprehension
// body) rather than being declarations of the node itself. For
// example, given:
//
//	x: {a: 1} & {b: 2}
//
// x's node has three Decls: the x field declaration itself, whose
// kind is not Embedded, and the two conjuncts, whose kinds are.
//
// Note that Embedded describes a Decl's own relationship to its node,
// not its position in the wider file: in
//
//	x: 3
//	{x: 4}
//
// both of x's Decls are plain field declarations ([DeclField], not
// Embedded); the embedded struct literal appears as a separate,
// Embedded, Decl ([DeclEmbedding]) of the root node.
func (k DeclKind) Embedded() bool {
	switch k {
	case DeclEmbedding, DeclConjunct, DeclDisjunct, DeclDefault, DeclComprehension:
		return true
	}
	return false
}

// String returns a short lower-case name for the kind, for
// diagnostics and tests.
func (k DeclKind) String() string {
	switch k {
	case DeclExpression:
		return "expression"
	case DeclFile:
		return "file"
	case DeclPackage:
		return "package"
	case DeclImport:
		return "import"
	case DeclField:
		return "field"
	case DeclPattern:
		return "pattern"
	case DeclDynamic:
		return "dynamic"
	case DeclAlias:
		return "alias"
	case DeclEmbedding:
		return "embedding"
	case DeclConjunct:
		return "conjunct"
	case DeclDisjunct:
		return "disjunct"
	case DeclDefault:
		return "default"
	case DeclComprehension:
		return "comprehension"
	case DeclEllipsis:
		return "ellipsis"
	}
	return fmt.Sprintf("DeclKind(%d)", int(k))
}

// Decl represents a single declaration that contributes to a [Node].
// A Node aggregates every declaration of a given field; each such
// declaration is exposed as a Decl, retaining its source key, its
// value expression, and any doc comments attached to it. Use
// [Node.Decls] to iterate the Decls of a node.
//
// A Decl is deliberately syntactic: its Value is a plain [ast.Node]
// which callers walk themselves (e.g. with [ast.Walk]). Whenever the
// walk encounters something that refers elsewhere (an ident, a
// selector expression, an index expression, an import spec, or a
// field's key) [Decl.Resolve] turns that element back into [Node]s,
// re-entering the semantic layer.
type Decl frame

// Key returns the [ast.Node] that names this declaration: typically
// the [ast.Ident] or [ast.BasicLit] used as the field label, or, for
// a pattern constraint or dynamic field carrying a value alias, the
// alias ident (a named field's Key is its name ident or literal,
// whatever its aliasing: X=y: 1 has Key y). Returns nil for
// declarations that have no source-level key, such as the file
// declarations exposed at the root, or conjunction and disjunction
// operands. See [Decl.Label] for a field's complete label, unaffected
// by aliasing.
func (d *Decl) Key() ast.Node {
	return d.key
}

// Label returns the complete [ast.Label] of the field declaration
// that this Decl models, with any value alias unwrapped: for an
// ordinary field the ident or string literal, for a pattern
// constraint the bracketed label, and for a dynamic field the
// parenthesized expression or interpolation. Unlike [Decl.Key], Label
// is unaffected by aliasing. For example, given:
//
//	v=[string]: {a: 1}
//	[int]: b: 2
//
// the first pattern's Key is the alias ident v, but its Label is the
// bracketed [string], so the actual pattern remains accessible; the
// second pattern's Key and Label are both the label [int].
//
// Label returns nil for declarations that do not model a field
// declaration (files, package clauses, imports, operands, ellipses,
// ...), and for fields synthesized by the evaluator (list elements).
func (d *Decl) Label() ast.Label {
	return d.label
}

// Value returns the [ast.Node] that holds this declaration's value:
// the field's right-hand-side expression for an ordinary field, the
// [ast.File] itself for a file-level declaration, or the operand
// expression for embedding-like Decls. Returns nil for declarations
// that have no associated value node (e.g. a package clause, whose
// information is exposed via [Decl.Key] and [Decl.DocComments] only,
// or a bare ellipsis).
func (d *Decl) Value() ast.Node {
	return d.node
}

// DocComments returns the doc-comment groups attached to this
// declaration, or nil if none are present.
func (d *Decl) DocComments() []*ast.CommentGroup {
	return ((*frame)(d)).docComments()
}

// Node returns the node to which this declaration contributes. Note
// that Decls merged into their node by embedding-like constructs all
// report the same containing node: in
//
//	x: {a: 1} & {b: 2}
//
// Node returns x's node for all three of x's Decls. Use [Decl.Kind],
// [Decl.Value] and [Decl.Fields] to tell such Decls apart.
func (d *Decl) Node() *Node {
	return (*Node)(d.navigable)
}

// Kind reports the syntactic construct that this declaration models.
// Use [DeclKind.Embedded] to test whether this declaration is merged
// into its node by an embedding-like construct.
func (d *Decl) Kind() DeclKind {
	return d.kind
}

// Fields yields the fields that this declaration alone contributes to
// its node, in lexical order of name. Whereas [Node.Fields] merges
// the contributions of every declaration, this is the per-declaration
// view, which recovers the grouping that merging discards. For
// example, given:
//
//	x: {a: 1} | {b: 2}
//
// x's node's Fields are a and b, but the [DeclDisjunct] Decl for {a:
// 1} yields only a, and the one for {b: 2} yields only b:
// distinguishing the branches of the disjunction.
//
// The same exclusions and naming rules apply as for [Node.Fields]:
// fields reached via references, pattern constraints, dynamic fields
// and list elements are not yielded; nor are lexical-only bindings
// (lets, field aliases, the names bound by imports), which bind names
// within this declaration but declare no field. The names yielded are
// the labels' string values, never their quoted source syntax, even
// for names that are not valid identifiers.
func (d *Decl) Fields() iter.Seq2[string, *Node] {
	return func(yield func(string, *Node) bool) {
		// Evaluating via the navigable (rather than f.eval directly)
		// preserves the invariants documented on [navigable.eval].
		d.navigable.eval()
		for _, name := range slices.Sorted(maps.Keys(d.bindings)) {
			if strings.HasPrefix(name, "__") {
				continue
			}
			nav := d.navigable.bindings[name]
			if nav == nil {
				continue
			}
			// Only yield navigable fields: a frame's bindings also
			// contain lexical-only bindings (aliases, lets, imports),
			// recognizable because they do not lead to the binding
			// registered in the navigable under the same name.
			isField := false
			for _, childFr := range d.bindings[name] {
				if childFr.navigable == nav {
					isField = true
					break
				}
			}
			if !isField {
				continue
			}
			if !yield(name, (*Node)(nav)) {
				return
			}
		}
	}
}

// Ellipses returns the nodes for the ellipses declared directly
// within this declaration. Whereas [Node.Ellipses] merges the
// ellipses of every declaration, this is the per-declaration view:
// in
//
//	x: {a: 1, ...} | {b: 2}
//
// only the [DeclDisjunct] Decl for {a: 1, ...} has an ellipsis,
// identifying which branch of the disjunction is open.
func (d *Decl) Ellipses() NodeSet {
	d.navigable.eval()
	result := make(NodeSet, len(d.ellipses))
	for i, nav := range d.ellipses {
		result[i] = (*Node)(nav)
	}
	return result
}

// Patterns returns the nodes for the pattern constraints declared
// directly within this declaration. Whereas [Node.Patterns] merges
// the patterns of every declaration, this is the per-declaration
// view: in
//
//	x: {[string]: 1} | {b: 2}
//
// only the [DeclDisjunct] Decl for {[string]: 1} has a pattern,
// identifying which branch of the disjunction declares it.
func (d *Decl) Patterns() NodeSet {
	d.navigable.eval()
	result := make(NodeSet, len(d.patterns))
	for i, nav := range d.patterns {
		result[i] = (*Node)(nav)
	}
	return result
}

// Dynamics returns the nodes for the dynamic fields declared directly
// within this declaration. Whereas [Node.Dynamics] merges the dynamic
// fields of every declaration, this is the per-declaration view. See
// [Node.Dynamics].
func (d *Decl) Dynamics() NodeSet {
	d.navigable.eval()
	result := make(NodeSet, len(d.dynamics))
	for i, nav := range d.dynamics {
		result[i] = (*Node)(nav)
	}
	return result
}

// Constraint reports this declaration's optionality marker:
// [ConstraintOptional] for an optional field (key?: value),
// [ConstraintRequired] for a required field (key!: value), and
// [ConstraintRegular] for a regular field and for every other kind of
// declaration. Beyond reporting the marker, the evaluator makes no
// use of optionality: an optional or required field declares its node
// exactly as a regular field does. Optionality belongs to an
// individual declaration, so different Decls of the same node can
// report different markers:
//
//	x?: 1
//	x: 2
//
// x's node has two Decls, reporting [ConstraintOptional] and
// [ConstraintRegular] respectively. See [Node.Constraint] for the
// effective constraint of the field across all of its declarations.
func (d *Decl) Constraint() Constraint {
	return d.constraint
}

// A Constraint is the optionality of a field declaration: regular,
// required (!), or optional (?). The constants are ordered by
// restrictiveness, most restrictive first, mirroring the ordering of
// the core evaluator's ArcType; the zero value is
// [ConstraintRegular]. See [Decl.Constraint] and [Node.Constraint].
type Constraint uint8

const (
	// ConstraintRegular is a plain field declaration: x: v.
	ConstraintRegular Constraint = iota
	// ConstraintRequired is a required field declaration: x!: v.
	ConstraintRequired
	// ConstraintOptional is an optional field declaration: x?: v.
	ConstraintOptional
)

func (c Constraint) String() string {
	switch c {
	case ConstraintRegular:
		return "regular"
	case ConstraintRequired:
		return "required"
	case ConstraintOptional:
		return "optional"
	}
	return fmt.Sprintf("Constraint(%d)", uint8(c))
}

// Token returns the source-level marker of the constraint:
// [token.NOT] for required (!), [token.OPTION] for optional (?), and,
// since a regular field has no marker, [token.ILLEGAL].
func (c Constraint) Token() token.Token {
	switch c {
	case ConstraintRequired:
		return token.NOT
	case ConstraintOptional:
		return token.OPTION
	}
	return token.ILLEGAL
}

// constraintFromToken converts the AST's representation of a field's
// optionality marker ([ast.Field.Constraint]) to a [Constraint].
func constraintFromToken(t token.Token) Constraint {
	switch t {
	case token.NOT:
		return ConstraintRequired
	case token.OPTION:
		return ConstraintOptional
	}
	return ConstraintRegular
}

// UnifyConstraints unifies field constraints. Field constraints form
// a subsumption order: an optional field (?, [ConstraintOptional])
// subsumes a required field (!, [ConstraintRequired]), which subsumes
// a regular field ([ConstraintRegular]). Unification takes the most
// specific: a regular declaration makes the field regular whatever
// the other declaration says, and otherwise required wins over
// optional. For example:
//
//	x?: 1
//	x!: 2
//
// declares a required field x, whereas
//
//	y?: 1
//	y: 2
//
// declares a regular field y. See [Decl.Constraint] and
// [Node.Constraint].
func UnifyConstraints(a, b Constraint) Constraint {
	// The constants are ordered by restrictiveness, most restrictive
	// first, so unification is simply the minimum.
	return min(a, b)
}

// Resolve resolves an expression element (which was found by walking
// this declaration's Key or Value) to the nodes to which it refers.
// The element MUST be one of the [ast.Node] values from this
// declaration's own syntax, otherwise the result will be nil.
// Resolve also returns nil for elements that do not resolve to
// anything: literals, operators, or expressions the evaluator does
// not track. Only as much of the package as is needed is evaluated.
//
// Within a path such as x.y.z, each component resolves separately:
// given
//
//	x: y: z: 3
//	w: x.y.z
//
// w's value is a selector expression, and walking it visits the
// expression's constituent parts, every one of which resolves: a
// selector (or index) expression resolves as its final component (its
// Sel or Index), so resolving the whole expression `x.y.z` (or
// equivalently the ident `z` within it) yields the node for x.y.z;
// resolving the interior sub-expression `x.y` (or the ident `y`
// within it) yields x.y; and resolving the leading ident `x` yields
// x. Only path elements resolve: wrapper expressions such as
// parentheses or a unary * default marker do not: walk inside them
// and resolve the path expression or ident within.
//
// A path may also be rooted at an inline expression rather than an
// ident:
//
//	v: {a: _}.a
//
// The inline struct is not a path element, and does not resolve.  The
// resolvable elements here are the ident `a` and the whole
// expression, both of which yield the node for the `a` field within
// the struct; the struct's own (anonymous) node is that node's
// [Node.Parent].
//
// In general an element can resolve to several nodes, e.g. when
// navigation traverses a reference:
//
//	x: y
//	x: z
//	y: a: 1
//	z: a: 2
//	w: x.a
//
// Here, resolving the ident `a` in w's selector value yields two
// nodes: y's `a` and z's `a`.
//
// Resolving a field declaration's key yields the node that the field
// declares - so for a Decl d of kind [DeclField], d.Resolve(d.Key())
// returns a set containing d.Node().  Resolving an ident whose
// binding is an alias, let, or import yields the (anonymous) node of
// that binding: use [Node.Expand] to see through it. Resolving an
// [ast.ImportSpec] yields the imported package's
// [Evaluator.PackageClauses] node.
//
// Resolve returns the direct targets only: it does not expand.
// Compose with [NodeSet.Expand] and [NodeSet.Fields] to navigate
// onwards from the result.
func (d *Decl) Resolve(el ast.Node) NodeSet {
	fe := d.fileEvaluator
	if el == nil || fe == nil {
		return nil
	}
	// A selector or index expression resolves as its final component:
	// interior expressions of a path are not themselves tracked as
	// path components, but their Sel/Index labels are, and yield the
	// resolution of the path prefix ending there. (For the outermost
	// expression this is identical to matching the whole-path
	// component below.)
	switch e := el.(type) {
	case *ast.SelectorExpr:
		el = e.Sel
	case *ast.IndexExpr:
		el = e.Index
	}
	if el == nil {
		return nil
	}
	pos := el.Pos()
	if !pos.IsValid() || pos.File() != fe.File.Pos().File() {
		return nil
	}
	offset := pos.Offset()
	leafFrames := fe.evalForOffset(offset)

	// Search by node identity: every element of a path that the
	// evaluator tracks is recorded, verbatim, as the node of a path
	// component (including field keys and whole path expressions), so
	// an element found by walking this declaration's syntax matches
	// exactly.
	var navs []*navigable
	for _, leafFr := range leafFrames {
		for _, p := range leafFr.childPaths {
			comps := p.components
			for i := range comps {
				if comps[i].node != el {
					continue
				}
				// The results of resolving a component are stored in the
				// component that follows it. The final component of a
				// multi-component path exists only to hold the results of
				// the whole path, so a match on it (the whole path
				// expression) - or on the single component of a length-1
				// path - finds its results in place.
				if i+1 < len(comps) {
					navs = append(navs, comps[i+1].unexpanded...)
				} else {
					navs = append(navs, comps[i].unexpanded...)
				}
				break
			}
		}
	}

	result := make(NodeSet, len(navs))
	for i, nav := range navs {
		result[i] = (*Node)(nav)
	}
	return dedupeNodes(result)
}
