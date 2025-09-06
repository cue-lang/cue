// Copyright 2025 CUE Authors
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

package trim

// # Overview
//
// The goal of trim is to remove redundant code within the supplied
// CUE ASTs.
//
// This is achieved by analysis of both the ASTs and the result of
// evaluation: looking at conjuncts etc within vertices. For each
// vertex, we try to identify conjuncts which are, by subsumption, as
// specific as the vertex as a whole. There three possible outcomes:
//
// a) No conjuncts on their own are found to be as specific as the
// vertex. In this case, we keep all the conjuncts. This is
// conservative, and may lead to conjuncts being kept which don't need
// to be, because we don't attempt to detect subsumption between
// subsets of a vertex's conjuncts. It is however safe.
//
// b) Exactly one conjunct is found which is as specific as the
// vertex. We keep this conjunct. Note that we do not currently
// consider that there may be other conjuncts within this vertex which
// have to be kept for other reasons, and in conjunction are as
// specific as this vertex. So again, we may end up keeping more
// conjuncts than strictly necessary, but it is still safe.
//
// c) Several conjuncts are found which are individually as specific
// as the vertex. We save this set of "winning conjuncts" for later.
//
// As we progress, we record the number of times each conjunct is seen
// (conjunct identity is taken as the conjunct's source node). Once we
// have completed traversing the vertices, we may have several sets of
// "winning conjuncts" each of which needs a conjunct selected to
// keep. We order these sets individually by seen-count (descending),
// and collectively by the sum of seen-counts for each set (also
// descending). For each set in turn, if there is no conjunct that is
// already kept, we choose to keep the most widely seen conjunct. If
// there is still a tie, we order by source code position.
//
// Additionally, if a conjunct survives, then we make sure that all
// references to that conjunct also survive. This helps to prevent
// surprises for the user: a field `x` that constrains a field `y`
// will always do so, even if `y` is always found to be more
// specific. For example:
//
//	x: >5
//	x: <10
//	y: 7
//	y: x
//
// Here, `y` will not be simplified to 7. By contrast,
//
//	y: >5
//	y: <10
//	y: 7
//
// will be simplified to `y: 7`.
//
// # Ignoring conjuncts
//
// When we inspect each vertex, there may be conjuncts that we must
// ignore for the purposes of finding conjuncts as specific as the
// vertex. The danger is that such conjuncts are found to be as
// specific as the whole vertex, thus causing the other conjuncts to
// be removed. But this can alter the semantics of the CUE code. For
// example, conjuncts that originate from within a disjunction branch
// must be ignored. Consider:
//
//	d: 6 | string
//	o: d & int
//
// The vertex for `o` will contain conjuncts for 6, and int. We would
// find the 6 is as specific as the vertex, so it is tempting to
// remove the `int`. But if we do, then the value of `o` changes
// because the string-branch of the disjunction can no longer be
// dismissed. Processing of disjunctions cannot be done on the AST,
// because disjunctions may contain references which we need to
// resolve, in order to know which conjuncts to ignore. For example:
//
//	d: c | string
//	o: d & int
//	c: 6
//
// Thus before we traverse the vertices to identify redundant
// conjuncts, we first traverse the vertices looking for disjunctions,
// and recording which conjuncts should be ignored.
//
// Another example is patterns: we must ignore conjuncts which are the
// roots of patterns. Consider:
//
//	[string]: 5
//	o: int
//
// In the vertex for `o` we would find conjuncts for 5 and `int`. We
// must ignore the 5, otherwise we would find that it is as specific
// as `o`, which could cause the entire field declaration `o: int` to
// be removed, which then changes the value of the CUE program.
//
// As with disjunctions, an earlier pass over the vertices identifies
// patterns and marks them accordingly.
//
// Finally, embedded values require special treatment. Consider:
//
//	x: y: 5
//	z: {
//		x
//	}
//
// Unfortunately, the evaluator doesn't track how different conjuncts
// arrive in a vertex: the vertex for `z` will not contain a conjunct
// which is a reference for `x`. All we will find in `z` is the arc
// for `y`. Because of this, we cannot discover that we must keep the
// embedded `x` -- it simply does not exist. So we take a rather blunt
// approach: an analysis of the AST will find where embeddings occur,
// which we record, and then when a vertex contains a struct which we
// know has an embedding, we always keep all the conjuncts in that
// vertex and its descendents.

import (
	"fmt"
	"io"
	"os"
	"slices"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/core/subsume"
	"cuelang.org/go/internal/value"
)

func filesV3(files []*ast.File, val cue.Value, cfg *Config) error {
	if err := val.Err(); err != nil {
		return err
	}
	inst := val.BuildInstance()
	if inst == nil {
		// Should not be nil, but just in case.
		return errors.Newf(val.Pos(), "trim: not a build instance")
	}
	dir := inst.Dir
	dir = strings.TrimRight(dir, string(os.PathSeparator)) +
		string(os.PathSeparator)

	if cfg.Trace && cfg.TraceWriter == nil {
		cfg.TraceWriter = os.Stderr
	}

	r, v := value.ToInternal(val)
	ctx := adt.NewContext(r, v)
	t := &trimmerV3{
		r:     r,
		ctx:   ctx,
		nodes: make(map[ast.Node]*nodeMeta),
		trace: cfg.TraceWriter,
	}

	t.logf("\nStarting trim in dir %q with files:", dir)
	for i, file := range files {
		t.logf(" %d: %s", i, file.Filename)
	}
	t.logf("\nFinding static dependencies")
	t.findStaticDependencies(files)
	t.logf("\nFinding patterns")
	t.findPatterns(v)
	t.logf("\nFinding disjunctions")
	t.findDisjunctions(v)
	t.logf("\nFinding redundances")
	t.findRedundancies(v, false)
	t.logf("\nSolve undecideds")
	t.solveUndecideds()

	t.logf("\nTrimming source")
	return t.trim(files, dir)
}

type nodeMeta struct {
	// The static parent - i.e. parent from the AST.
	parent *nodeMeta

	src ast.Node

	// If true, then this node must not be removed, because it is not
	// redundant in at least one place where it's used.
	required bool

	// If true, then conjuncts of this node should be ignored for the
	// purpose of testing for redundant conjuncts.
	ignoreConjunct bool

	// If this is true then this node has one or more embedded values
	// (statically) - i.e. EmbedDecl has been found within this node
	// (and src will be either a File or a StructLit).
	hasEmbedding bool

	// If x.requiredBy = {y,z} then it means x must be kept if one or
	// more of {y,z} are kept. It is directional: if x must be kept for
	// other reasons, then that says nothing about whether any of {y,z}
	// must be kept.
	requiredBy []*nodeMeta

	// The number of times conjuncts of this node have been found in
	// the vertices. This is used for choosing winning conjuncts, and
	// to ensure that we never remove a node which we have only seen in
	// the AST, and not in result of evaluation.
	seenCount int
}

func (nm *nodeMeta) incSeenCount() {
	nm.seenCount++
}

func (nm *nodeMeta) markRequired() {
	nm.required = true
}

func (nm *nodeMeta) addRequiredBy(e *nodeMeta) {
	if slices.Contains(nm.requiredBy, e) {
		return
	}
	nm.requiredBy = append(nm.requiredBy, e)
}

func (a *nodeMeta) isRequiredBy(b *nodeMeta) bool {
	if a == b {
		return true
	}
	return a._isRequiredBy(map[*nodeMeta]struct{}{a: {}}, b)
}

// Need to cope with cycles, hence the seen/visited-set.
func (a *nodeMeta) _isRequiredBy(seen map[*nodeMeta]struct{}, b *nodeMeta) bool {
	for _, e := range a.requiredBy {
		if e == b {
			return true
		}
		if _, found := seen[e]; found {
			continue
		}
		seen[e] = struct{}{}
		if e._isRequiredBy(seen, b) {
			return true
		}
	}
	return false
}

// True iff this node is required, or any of the nodes that require
// this node are themselves required (transitively).
func (nm *nodeMeta) isRequired() bool {
	if nm.required {
		return true
	}
	if len(nm.requiredBy) == 0 {
		return false
	}
	return nm._isRequired(map[*nodeMeta]struct{}{nm: {}})
}

func (nm *nodeMeta) _isRequired(seen map[*nodeMeta]struct{}) bool {
	if nm.required {
		return true
	}
	for _, e := range nm.requiredBy {
		if _, found := seen[e]; found {
			continue
		}
		seen[e] = struct{}{}
		if e._isRequired(seen) {
			nm.required = true
			return true
		}
	}
	return false
}

// True iff this node or any of its parent nodes (static/AST parents),
// have been identified as containing embedded values.
func (nm *nodeMeta) isEmbedded() bool {
	for ; nm != nil; nm = nm.parent {
		if nm.hasEmbedding {
			return true
		}
	}
	return false
}

// True iff a is an ancestor of b (in the static/AST parent-child
// sense).
func (a *nodeMeta) isAncestorOf(b *nodeMeta) bool {
	if a == nil {
		return false
	}
	for b != nil {
		if b == a {
			return true
		}
		b = b.parent
	}
	return false
}

type trimmerV3 struct {
	r     *runtime.Runtime
	ctx   *adt.OpContext
	nodes map[ast.Node]*nodeMeta

	undecided []nodeMetas

	// depth is purely for debugging trace indentation level.
	depth int
	trace io.Writer
}

func (t *trimmerV3) logf(format string, args ...any) {
	w := t.trace
	if w == nil {
		return
	}
	fmt.Fprintf(w, "%*s", t.depth*3, "")
	fmt.Fprintf(w, format, args...)
	fmt.Fprintln(w)
}

func (t *trimmerV3) inc() { t.depth++ }
func (t *trimmerV3) dec() { t.depth-- }

func (t *trimmerV3) getNodeMeta(n ast.Node) *nodeMeta {
	if n == nil {
		return nil
	}
	d, found := t.nodes[n]
	if !found {
		d = &nodeMeta{src: n}
		t.nodes[n] = d
	}
	return d
}

// Discovers findStaticDependencies between nodes by walking through the AST of
// the files.
//
// 1. Establishes that if a node survives then its parent must also
// survive. I.e. a parent is required by its children.
//
// 2. Marks the arguments for call expressions as required: no
// simplification can occur there. This is because we cannot discover
// the relationship between arguments to a function and the function's
// result, and so any simplification of the arguments may change the
// result of the function call in unknown ways.
//
// 3. The conjuncts in a adt.Vertex do not give any information as to
// whether they have arrived via embedding or not. But, in the AST, we
// do have that information. So find and record embedding information.
func (t *trimmerV3) findStaticDependencies(files []*ast.File) {
	t.inc()
	defer t.dec()

	var ancestors []*nodeMeta
	callCount := 0
	for _, f := range files {
		t.logf("%s", f.Filename)
		ast.Walk(f, func(n ast.Node) bool {
			t.inc()
			t.logf("%p::%T %v", n, n, n.Pos())
			nm := t.getNodeMeta(n)
			if field, ok := n.(*ast.Field); ok {
				switch field.Constraint {
				case token.NOT, token.OPTION:
					t.logf(" ignoring %v", nm.src.Pos())
					nm.ignoreConjunct = true
					nm.markRequired()
				}
			}
			if l := len(ancestors); l > 0 {
				parent := ancestors[l-1]
				parent.addRequiredBy(nm)
				nm.parent = parent
			}
			ancestors = append(ancestors, nm)
			if _, ok := n.(*ast.CallExpr); ok {
				callCount++
			}
			if callCount > 0 {
				// This is somewhat unfortunate, but for now, as soon as
				// we're in the arguments for a function call, we prevent
				// all simplifications.
				nm.markRequired()
			}
			if _, ok := n.(*ast.EmbedDecl); ok && nm.parent != nil {
				// The parent of an EmbedDecl is always either a File or a
				// StructLit.
				nm.parent.hasEmbedding = true
			}
			return true
		}, func(n ast.Node) {
			if _, ok := n.(*ast.CallExpr); ok {
				callCount--
			}
			ancestors = ancestors[:len(ancestors)-1]
			t.dec()
		})
	}
}

// Discovers patterns by walking vertices and their arcs recursively.
//
// Conjuncts that originate from the pattern constraint must be
// ignored when searching for redundancies, otherwise they can be
// found to be more-or-equally-specific than the vertex in which
// they're found, and could lead to the entire field being
// removed. These conjuncts must also be kept because even if the
// pattern is not actually used, it may form part of the public API of
// the CUE, and so removing an unused pattern may alter the API.
//
// We only need to mark the conjuncts at the "top level" of the
// pattern constraint as required+ignore; we do not need to descend
// into the arcs of the pattern constraint. This is because the
// pattern only matches against a key, and not a path. So, even with:
//
//	a: [string]: x: y: z: 5
//
// we only need to mark the x as required+ignore, and not the y, z, or
// 5. This ensures we later ignore only this x when simplifying other
// conjuncts in a vertex who's label has matched this pattern. If we
// add:
//
//	b: w: x: y: {}
//	b: a
//
// This will get trimmed to:
//
//	a: [string]: x: y: z: 5
//	b: w: _
//	b: a
//
// I.e. by ignoring the pattern`s "top level" conjuncts, we ensure we
// keep b: w, even though the pattern is equally specific to the
// vertex for b.w, and the explicit b: w (from line 2) is less
// specific.
func (t *trimmerV3) findPatterns(v *adt.Vertex) {
	t.inc()
	defer t.dec()

	worklist := []*adt.Vertex{v}
	for len(worklist) != 0 {
		v := worklist[0]
		worklist = worklist[1:]

		t.logf("vertex %p; kind %v; value %p::%T",
			v, v.Kind(), v.BaseValue, v.BaseValue)
		t.inc()

		if patterns := v.PatternConstraints; patterns != nil {
			for i, pair := range patterns.Pairs {
				pair.Constraint.Finalize(t.ctx)
				t.logf("pattern %d %p::%T", i, pair.Constraint, pair.Constraint)
				t.inc()
				pair.Constraint.VisitLeafConjuncts(func(c adt.Conjunct) bool {
					field := c.Field()
					elem := c.Elem()
					expr := c.Expr()
					t.logf("conjunct field: %p::%T, elem: %p::%T, expr: %p::%T",
						field, field, elem, elem, expr, expr)

					if src := field.Source(); src != nil {
						nm := t.getNodeMeta(src)
						t.logf(" ignoring %v", nm.src.Pos())
						nm.ignoreConjunct = true
						nm.markRequired()
					}

					return true
				})
				t.dec()
			}
		}

		t.dec()

		worklist = append(worklist, v.Arcs...)
		if v, ok := v.BaseValue.(*adt.Vertex); ok {
			worklist = append(worklist, v)
		}
	}
}

// Discovers disjunctions by walking vertices and their arcs
// recursively.
//
// Disjunctions and their branches must be found before we attempt to
// simplify vertices. We must find disjunctions and mark all conjuncts
// within each branch of a disjunction, including all conjuncts that
// can be reached via resolution, as required+ignore.
//
// Failure to do this can lead to the removal of conjuncts in a vertex
// which were essential for discriminating between branches of a
// disjunction.
func (t *trimmerV3) findDisjunctions(v *adt.Vertex) {
	t.inc()
	defer t.dec()

	var branches []*adt.Vertex
	seen := make(map[*adt.Vertex]struct{})
	worklist := []*adt.Vertex{v}
	for len(worklist) != 0 {
		v := worklist[0]
		worklist = worklist[1:]

		if _, found := seen[v]; found {
			continue
		}
		seen[v] = struct{}{}

		t.logf("vertex %p; kind %v; value %p::%T",
			v, v.Kind(), v.BaseValue, v.BaseValue)
		t.inc()

		if disj, ok := v.BaseValue.(*adt.Disjunction); ok {
			t.logf("found disjunction in basevalue")
			for i, val := range disj.Values {
				t.logf("branch %d", i)
				if v, ok := val.(*adt.Vertex); ok {
					branches = append(branches, v)
				}
			}
		}

		v.VisitLeafConjuncts(func(c adt.Conjunct) bool {
			switch disj := c.Elem().(type) {
			case *adt.Disjunction:
				t.logf("found disjunction")
				for i, val := range disj.Values {
					t.logf("branch %d", i)
					branch := &adt.Vertex{
						Parent: v.Parent,
						Label:  v.Label,
					}
					c := adt.MakeConjunct(c.Env, val, c.CloseInfo)
					branch.InsertConjunct(c)
					branch.Finalize(t.ctx)
					branches = append(branches, branch)
				}

			case *adt.DisjunctionExpr:
				t.logf("found disjunctionexpr")
				for i, val := range disj.Values {
					t.logf("branch %d", i)
					branch := &adt.Vertex{
						Parent: v.Parent,
						Label:  v.Label,
					}
					c := adt.MakeConjunct(c.Env, val.Val, c.CloseInfo)
					branch.InsertConjunct(c)
					branch.Finalize(t.ctx)
					branches = append(branches, branch)
				}
			}
			return true
		})

		t.dec()

		worklist = append(worklist, v.Arcs...)
		if v, ok := v.BaseValue.(*adt.Vertex); ok {
			worklist = append(worklist, v)
		}
	}

	clear(seen)
	worklist = branches
	for len(worklist) != 0 {
		v := worklist[0]
		worklist = worklist[1:]

		if _, found := seen[v]; found {
			continue
		}
		seen[v] = struct{}{}

		v.VisitLeafConjuncts(func(c adt.Conjunct) bool {
			if src := c.Field().Source(); src != nil {
				nm := t.getNodeMeta(src)
				t.logf(" ignoring %v", nm.src.Pos())
				nm.ignoreConjunct = true
				nm.markRequired()
			}
			t.resolveElemAll(c, func(resolver adt.Resolver, resolvedTo *adt.Vertex) {
				worklist = append(worklist, resolvedTo.Arcs...)
			})
			return true
		})
		worklist = append(worklist, v.Arcs...)
	}
}

func (t *trimmerV3) keepAllChildren(n ast.Node) {
	ast.Walk(n, func(n ast.Node) bool {
		nm := t.getNodeMeta(n)
		nm.markRequired()
		return true
	}, nil)
}

// Once we have identified, and masked out, call expressions,
// embeddings, patterns, and disjunctions, we can finally work
// recursively through the vertices, testing their conjuncts to find
// redundant conjuncts.
func (t *trimmerV3) findRedundancies(v *adt.Vertex, keepAll bool) {
	v = v.DerefDisjunct()
	t.logf("vertex %p (parent %p); kind %v; value %p::%T",
		v, v.Parent, v.Kind(), v.BaseValue, v.BaseValue)
	t.inc()
	defer t.dec()

	_, isDisjunct := v.BaseValue.(*adt.Disjunction)
	for _, si := range v.Structs {
		if src := si.StructLit.Src; src != nil {
			t.logf("struct lit %p src: %p::%T %v", si.StructLit, src, src, src.Pos())
			nm := t.getNodeMeta(src)
			nm.incSeenCount()
			keepAll = keepAll || nm.isEmbedded()
			if nm.hasEmbedding {
				t.logf(" (has embedding root)")
			}
			if nm.isEmbedded() {
				t.logf(" (isEmbedded)")
			} else if keepAll {
				t.logf(" (keepAll)")
			}

			if !isDisjunct {
				continue
			}
			v1 := &adt.Vertex{
				Parent: v.Parent,
				Label:  v.Label,
			}
			c := adt.MakeConjunct(si.Env, si.StructLit, si.CloseInfo)
			v1.InsertConjunct(c)
			v1.Finalize(t.ctx)
			t.logf("exploring disj struct lit %p (src %v): start", si, src.Pos())
			t.findRedundancies(v1, keepAll)
			t.logf("exploring disj struct lit %p (src %v): end", si, src.Pos())
		}
	}

	if keepAll {
		for _, si := range v.Structs {
			if src := si.StructLit.Src; src != nil {
				t.keepAllChildren(src)
			}
		}
	}

	if patterns := v.PatternConstraints; patterns != nil {
		for i, pair := range patterns.Pairs {
			pair.Constraint.Finalize(t.ctx)
			t.logf("pattern %d %p::%T", i, pair.Constraint, pair.Constraint)
			t.findRedundancies(pair.Constraint, keepAll)
		}
	}

	var nodeMetas, winners, disjDefaultWinners []*nodeMeta
	v.VisitLeafConjuncts(func(c adt.Conjunct) bool {
		field := c.Field()
		elem := c.Elem()
		expr := c.Expr()
		src := field.Source()
		if src == nil {
			t.logf("conjunct field: %p::%T, elem: %p::%T, expr: %p::%T, src nil",
				field, field, elem, elem, expr, expr)
			return true
		}

		t.logf("conjunct field: %p::%T, elem: %p::%T, expr: %p::%T, src: %v",
			field, field, elem, elem, expr, expr, src.Pos())

		nm := t.getNodeMeta(src)
		nm.incSeenCount()

		// Currently we replace redundant structs with _. If it becomes
		// desired to replace them with {} instead, then we want this
		// code instead of the block that follows:
		//
		// if exprSrc := expr.Source(); exprSrc != nil {
		// 	exprNm := t.getNodeMeta(exprSrc)
		// 	exprNm.addRequiredBy(nm)
		// }
		if exprSrc := expr.Source(); exprSrc != nil && len(v.Arcs) == 0 {
			switch expr.(type) {
			case *adt.StructLit, *adt.ListLit:
				t.logf(" saving emptyness")
				exprNm := t.getNodeMeta(exprSrc)
				exprNm.addRequiredBy(nm)
			}
		}

		if nm.ignoreConjunct {
			t.logf(" ignoring conjunct")
		} else {
			nodeMetas = append(nodeMetas, nm)
			if t.equallySpecific(v, c) {
				winners = append(winners, nm)
				t.logf(" equally specific: %p::%T", field, field)
			} else {
				t.logf(" redundant here: %p::%T", field, field)
			}
		}

		if disj, ok := expr.(*adt.DisjunctionExpr); !nm.ignoreConjunct && ok && disj.HasDefaults {
			defaultCount := 0
			matchingDefaultCount := 0
			for _, branch := range disj.Values {
				if !branch.Default {
					continue
				}
				defaultCount++
				c := adt.MakeConjunct(c.Env, branch.Val, c.CloseInfo)
				if t.equallySpecific(v, c) {
					matchingDefaultCount++
				}
			}
			if defaultCount > 0 && defaultCount == matchingDefaultCount {
				t.logf(" found %d matching defaults in disjunction",
					matchingDefaultCount)
				disjDefaultWinners = append(disjDefaultWinners, nm)
			}
		}

		if compr, ok := elem.(*adt.Comprehension); ok {
			t.logf("comprehension found")
			for _, clause := range compr.Clauses {
				var conj adt.Conjunct
				switch clause := clause.(type) {
				case *adt.IfClause:
					conj = adt.MakeConjunct(c.Env, clause.Condition, c.CloseInfo)
				case *adt.ForClause:
					conj = adt.MakeConjunct(c.Env, clause.Src, c.CloseInfo)
				case *adt.LetClause:
					conj = adt.MakeConjunct(c.Env, clause.Expr, c.CloseInfo)
				}
				t.linkResolvers(conj, true)
			}
		}

		t.linkResolvers(c, false)
		return true
	})

	if keepAll {
		t.logf("keeping all %d nodes", len(nodeMetas))
		for _, d := range nodeMetas {
			t.logf(" %p::%T %v", d.src, d.src, d.src.Pos())
			d.markRequired()
		}

	} else {
		if len(disjDefaultWinners) != 0 {
			// For all the conjuncts that were disjunctions and contained
			// defaults, and *every* default is equally specific as the
			// vertex as a whole, then we should be able to ignore all
			// other winning conjuncts.
			winners = disjDefaultWinners
		}
		switch len(winners) {
		case 0:
			t.logf("no winners; keeping all %d nodes", len(nodeMetas))
			for _, d := range nodeMetas {
				t.logf(" %p::%T %v", d.src, d.src, d.src.Pos())
				d.markRequired()
			}

		case 1:
			t.logf("1 winner")
			src := winners[0].src
			t.logf(" %p::%T %v", src, src, src.Pos())
			winners[0].markRequired()

		default:
			t.logf("%d winners found", len(winners))
			foundRequired := false
			for _, d := range winners {
				if d.isRequired() {
					foundRequired = true
					break
				}
			}
			if !foundRequired {
				t.logf("no winner already required")
				t.undecided = append(t.undecided, winners)
			}
		}
	}

	for i, a := range v.Arcs {
		t.logf("arc %d %v", i, a.Label)
		t.findRedundancies(a, keepAll)
	}

	if v, ok := v.BaseValue.(*adt.Vertex); ok && v != nil {
		t.logf("exploring base value: start")
		t.findRedundancies(v, keepAll)
		t.logf("exploring base value: end")
	}
}

// If somewhere within a conjunct, there's a *[adt.FieldReference], or
// other type of [adt.Resolver], then we need to find that, and ensure
// that:
//
//  1. if the resolver part of this conjunct survives, then the target
//     of the resolver must survive too (i.e. we don't create dangling
//     pointers). This bit is done for free, because if a vertex
//     contains a conjunct for some reference `r`, then whatever `r`
//     resolved to will also appear in this vertex's conjuncts.
//
//  2. if the target of the resolver survives, then we must
//     survive. This enforces the basic rule that if a conjunct
//     survives then all the references to that conjunct must also
//     survive.
func (t *trimmerV3) linkResolvers(c adt.Conjunct, addInverse bool) {
	var origNm *nodeMeta
	if src := c.Field().Source(); src != nil {
		origNm = t.getNodeMeta(src)
	}

	t.resolveElemAll(c, func(resolver adt.Resolver, resolvedTo *adt.Vertex) {
		resolvedTo.VisitLeafConjuncts(func(resolvedToC adt.Conjunct) bool {
			src := resolvedToC.Source()
			if src == nil {
				return true
			}
			resolvedToNm := t.getNodeMeta(src)
			resolverNm := t.getNodeMeta(resolver.Source())

			// If the resolvedToC conjunct survives, then the resolver
			// itself must survive too.
			resolverNm.addRequiredBy(resolvedToNm)
			t.logf("  (regular) %v reqBy %v",
				resolverNm.src.Pos(), resolvedToNm.src.Pos())
			if addInverse {
				t.logf("  (inverse) %v reqBy %v",
					resolvedToNm.src.Pos(), resolverNm.src.Pos())
				resolvedToNm.addRequiredBy(resolverNm)
			}

			// Don't break lexical scopes. Consider:
			//
			//	c: {
			//		x: int
			//		y: x
			//	}
			//	c: x: 5
			//
			// We must make sure that if `y: x` survives, then `x:
			// int` survives (or at least the field does - it could
			// be simplified to `x: _`) *even though* there is a
			// more specific value for c.x in the final line. Thus
			// the field which we have found by resolution, is
			// required by the original element.
			if origNm != nil &&
				resolvedToNm.parent.isAncestorOf(origNm) {
				t.logf("  (extra) %v reqBy %v",
					resolvedToNm.src.Pos(), origNm.src.Pos())
				resolvedToNm.addRequiredBy(origNm)
			}
			return true
		})
	})
}

func (t *trimmerV3) resolveElemAll(c adt.Conjunct, f func(adt.Resolver, *adt.Vertex)) {
	worklist := []adt.Elem{c.Elem()}
	for len(worklist) != 0 {
		elem := worklist[0]
		worklist = worklist[1:]

		switch elemT := elem.(type) {
		case *adt.UnaryExpr:
			worklist = append(worklist, elemT.X)
		case *adt.BinaryExpr:
			worklist = append(worklist, elemT.X, elemT.Y)
		case *adt.DisjunctionExpr:
			for _, disjunct := range elemT.Values {
				worklist = append(worklist, disjunct.Val)
			}
		case *adt.Disjunction:
			for _, disjunct := range elemT.Values {
				worklist = append(worklist, disjunct)
			}
		case *adt.Ellipsis:
			worklist = append(worklist, elemT.Value)
		case *adt.BoundExpr:
			worklist = append(worklist, elemT.Expr)
		case *adt.BoundValue:
			worklist = append(worklist, elemT.Value)
		case *adt.Interpolation:
			for _, part := range elemT.Parts {
				worklist = append(worklist, part)
			}
		case *adt.Conjunction:
			for _, val := range elemT.Values {
				worklist = append(worklist, val)
			}
		case *adt.CallExpr:
			worklist = append(worklist, elemT.Fun)
			for _, arg := range elemT.Args {
				worklist = append(worklist, arg)
			}
		case *adt.Comprehension:
			for _, y := range elemT.Clauses {
				switch y := y.(type) {
				case *adt.IfClause:
					worklist = append(worklist, y.Condition)
				case *adt.LetClause:
					worklist = append(worklist, y.Expr)
				case *adt.ForClause:
					worklist = append(worklist, y.Src)
				}
			}
		case *adt.LabelReference:
			elem = &adt.ValueReference{UpCount: elemT.UpCount, Src: elemT.Src}
			t.logf(" converting LabelReference to ValueReference")
		}

		if r, ok := elem.(adt.Resolver); ok && elem.Source() != nil {
			resolvedTo, bot := t.ctx.Resolve(c, r)
			if bot != nil {
				continue
			}
			t.logf(" resolved to %p", resolvedTo)
			f(r, resolvedTo)
		}
	}
}

// Are all the cs combined, (more or) equally as specific as v?
func (t *trimmerV3) equallySpecific(v *adt.Vertex, cs ...adt.Conjunct) bool {
	t.inc()
	//	t.ctx.LogEval = 1
	conjVertex := &adt.Vertex{
		Parent: v.Parent,
		Label:  v.Label,
	}
	for _, c := range cs {
		if r, ok := c.Elem().(adt.Resolver); ok {
			v1, bot := t.ctx.Resolve(c, r)
			if bot == nil {
				v1.VisitLeafConjuncts(func(c adt.Conjunct) bool {
					conjVertex.InsertConjunct(c)
					return true
				})
				continue
			}
		}
		conjVertex.InsertConjunct(c)
	}
	conjVertex.Finalize(t.ctx)
	err := subsume.Value(t.ctx, v, conjVertex)
	if err != nil {
		t.logf(" not equallySpecific")
		if t.trace != nil && t.ctx.LogEval > 0 {
			errors.Print(t.trace, err, nil)
		}
	}
	//	t.ctx.LogEval = 0
	t.dec()
	return err == nil
}

// NB this is not perfect. We do not attempt to track dependencies
// *between* different sets of "winning" nodes.
//
// We could have two sets, [a, b, c] and [c, d], and decide here to
// require a from the first set, and then c from the second set. This
// preserves more nodes than strictly necessary (preserving c on its
// own is sufficient to satisfy both sets). However, doing this
// perfectly is the “Hitting Set Problem”, and it is proven
// NP-complete. Thus for efficiency, we consider each set (more or
// less) in isolation.
func (t *trimmerV3) solveUndecideds() {
	if len(t.undecided) == 0 {
		return
	}
	undecided := t.undecided
	for i, ds := range undecided {
		ds.sort()
		if ds.hasRequired() {
			undecided[i] = nil
		}
	}

	slices.SortFunc(undecided, func(as, bs nodeMetas) int {
		aSum, bSum := as.seenCountSum(), bs.seenCountSum()
		if aSum != bSum {
			return bSum - aSum
		}
		aLen, bLen := len(as), len(bs)
		if aLen != bLen {
			return bLen - aLen
		}
		for i, a := range as {
			b := bs[i]
			if posCmp := a.src.Pos().Compare(b.src.Pos()); posCmp != 0 {
				return posCmp
			}
		}
		return 0
	})

	for _, nms := range undecided {
		if len(nms) == 0 {
			// once we get to length of 0, everything that follows must
			// also be length of 0
			break
		}
		t.logf("choosing winner from %v", nms)
		if nms.hasRequired() {
			t.logf(" already contains required node")
			continue
		}

		nms[0].markRequired()
	}
}

type nodeMetas []*nodeMeta

// Sort a single set of nodeMetas. If a set contains x and y:
//
// - if x is required by y, then x will come first;
// - otherwise whichever node has a higher seenCount comes first;
// - otherwise sort x and y by their src position.
func (nms nodeMetas) sort() {
	slices.SortFunc(nms, func(a, b *nodeMeta) int {
		if a.isRequiredBy(b) {
			return -1
		}
		if b.isRequiredBy(a) {
			return 1
		}
		aSeen, bSeen := a.seenCount, b.seenCount
		if aSeen != bSeen {
			return bSeen - aSeen
		}
		return a.src.Pos().Compare(b.src.Pos())
	})
}

func (nms nodeMetas) seenCountSum() (sum int) {
	for _, d := range nms {
		sum += d.seenCount
	}
	return sum
}

func (nms nodeMetas) hasRequired() bool {
	for _, d := range nms {
		if d.isRequired() {
			return true
		}
	}
	return false
}

// After all the analysis is complete, trim finally modifies the AST,
// removing (or simplifying) nodes which have not been found to be
// required.
func (t *trimmerV3) trim(files []*ast.File, dir string) error {
	t.inc()
	defer t.dec()

	for _, f := range files {
		if !strings.HasPrefix(f.Filename, dir) {
			continue
		}
		t.logf("%s", f.Filename)
		t.inc()
		astutil.Apply(f, func(c astutil.Cursor) bool {
			n := c.Node()
			d := t.nodes[n]

			if !d.isRequired() && d.seenCount > 0 {
				// The astutils cursor only supports deleting nodes if the
				// node is a child of a structlit or a file. So in all
				// other cases, we must replace the child with top.
				var replacement ast.Node = ast.NewIdent("_")
				if d.parent != nil {
					switch parentN := d.parent.src.(type) {
					case *ast.File, *ast.StructLit:
						replacement = nil
					case *ast.Comprehension:
						if n == parentN.Value {
							replacement = ast.NewStruct()
						}
					}
				}
				if replacement == nil {
					t.logf("deleting node %p::%T %v", n, n, n.Pos())
					c.Delete()
				} else {
					t.logf("replacing node %p::%T with %T %v",
						n, n, replacement, n.Pos())
					c.Replace(replacement)
				}
			}

			return true
		}, nil)
		if err := astutil.Sanitize(f); err != nil {
			return err
		}
		t.dec()
	}
	return nil
}
