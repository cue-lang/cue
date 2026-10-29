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

package fix

import (
	"slices"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
)

// todoComment marks a rewrite which changed what the code means, for the
// user to review.
func todoComment(msg string) *ast.CommentGroup {
	return fixComment("TODO", msg)
}

// noteComment marks a rewrite which the fixer could not tell was needed,
// and which the user may undo. Unlike a TODO it does not call for review.
func noteComment(msg string) *ast.CommentGroup {
	return fixComment("NOTE", msg)
}

func fixComment(kind, msg string) *ast.CommentGroup {
	// Wrap the message across several comment lines so no line runs much
	// past 80 columns. The indentation the formatter adds is not counted,
	// so a deeply nested comment may still spill a little.
	const width = 80
	var list []*ast.Comment
	line := "// " + kind + "(cue-fix):"
	for _, w := range strings.Fields(msg) {
		if len(line)+1+len(w) > width {
			list = append(list, &ast.Comment{Text: line})
			line = "//"
		}
		line += " " + w
	}
	list = append(list, &ast.Comment{Text: line})
	return &ast.CommentGroup{Doc: true, List: list}
}

// embedFlags tracks what kind of closing an embedding requires.
type embedFlags struct {
	def          bool // a definition was embedded
	other        bool // another embedding was modified (needs runtime check)
	forceReclose bool // disjunction with a non-definition operand; needs __reclose
	close        bool // close() was embedded and hoisted to wrapper level
}

func (a embedFlags) or(b embedFlags) embedFlags {
	return embedFlags{
		def:          a.def || b.def,
		other:        a.other || b.other,
		forceReclose: a.forceReclose || b.forceReclose,
		close:        a.close || b.close,
	}
}

// mayBeClosed reports whether the expression the flags were collected
// from may resolve to a closed value.
func (f embedFlags) mayBeClosed() bool {
	return f.def || f.other || f.close
}

// isDefinition reports whether the expression the flags were collected
// from is a definition, or a disjunction of definitions, and so closed
// recursively whatever it resolves to.
func (f embedFlags) isDefinition() bool {
	return f == embedFlags{def: true}
}

type closeInfo struct {
	// Do not close enclosing structs if non-zero. This may be the case
	// for comprehensions, nested structs, etc.
	suspendReclose int
	// The scope is the value of a field with a definition label, which
	// closes that value on its own. Not inherited.
	defLabel bool

	// The scope is lexically inside a definition field, outside any let
	// value, disjunction operand or call argument, which a definition
	// does not close on their own. A nested literal there still takes
	// its wrapper: the wrapper closes the literal on its own, where the
	// definition closes what all its conjuncts declare together, and the
	// two differ once the literal is selected from a unified value or
	// combined with a disjunction. The one exception is a literal with a
	// comprehension guard reading the literal's own field through an
	// alias of an enclosing field: a wrapper builtin's argument cannot
	// resolve such a guard, so there the definition's closing has to do.
	// Like inComprehension it is inherited by nested scopes (see
	// pushScope).
	inDefinition bool

	// The value aliases of the enclosing fields, innermost last, which a
	// guard inside a nested literal may read a field through. Inherited
	// by nested scopes.
	aliases []*ast.Alias

	// The scope is lexically inside a comprehension value. Unlike the
	// rest of closeInfo, it is inherited by nested scopes (see pushScope).
	inComprehension bool

	// An embedding declared directly in the struct literal being
	// traversed is that literal's whole value: the literal has no other
	// element, and the literal is itself the whole value of the position
	// it appears in. Such an embedding needs no opening, as { X } is
	// equivalent to X — the literal declares nothing of its own which
	// the old semantics opened X for. Unlike suspendReclose it follows
	// from the position of the literal, so it carries through the
	// embedding of a nested literal.
	wholeValue bool

	embedFlags
}

func (c closeInfo) shouldReclose() bool {
	return c.suspendReclose == 0
}

func fixExplicitOpen(f *ast.File) (*ast.File, bool) {
	var hasChanges bool
	// Comprehension fields which the old semantics closed, and which
	// openCompFieldValue must therefore leave alone.
	closedCompFields := oldClosedCompFields(f)

	var info closeInfo
	recloseStack := []closeInfo{}
	// pushScope and popScope bracket the traversal of a node that starts
	// a new reclose scope: fields, conjunctions and disjunctions, and
	// comprehensions.
	pushScope := func(next closeInfo) {
		next.inComprehension = next.inComprehension || info.inComprehension
		next.inDefinition = next.inDefinition || info.inDefinition
		if next.aliases == nil {
			next.aliases = info.aliases
		}
		recloseStack = append(recloseStack, info)
		info = next
	}
	popScope := func(c astutil.Cursor) {
		info = recloseStack[len(recloseStack)-1]
		recloseStack = recloseStack[:len(recloseStack)-1]
		c.ClearEnclosingModified()
	}
	// Each struct literal collects the embedFlags of its own embedded
	// declarations and decides its wrapper from exactly those: flags
	// must not leak to sibling literals in the same scope. Enclosing
	// literals re-collect them through collectEmbedFlags, which
	// descends into embedded literals. litStack saves the enclosing
	// literal's flags, and its wholeValue, which soleEmbed re-decides
	// per literal.
	var litStack []closeInfo
	return astutil.Apply(f, func(c astutil.Cursor) bool {
		n := c.Node()
		switch n := n.(type) {
		case *ast.Field:
			// A field's value is not embedded in anything, so a literal
			// there is the whole value of its position. Inside a
			// comprehension it is not: the old semantics opened the
			// conjunct the comprehension inserted, whatever it declared
			// (see openCompFieldValue).
			next := closeInfo{wholeValue: !info.inComprehension}
			// Fields with definition labels reclose on their own. Fields
			// inside comprehensions never wrap: a wrapper would deny
			// fields that the old semantics allowed (see
			// openCompFieldValue).
			if internal.IsDefinition(n.Label) {
				next.defLabel = true
				next.inDefinition = true
			}
			if next.defLabel || info.inComprehension {
				next.suspendReclose = 1
			}
			pushScope(next)

		case *ast.BinaryExpr:
			if n.Op == token.AND || n.Op == token.OR {
				pushScope(closeInfo{wholeValue: true})
				// An operand keeps its wrapper even inside a definition:
				// the old semantics closed it on its own, and in a
				// disjunction that is what eliminates a branch, so the
				// fields nested in the operands keep theirs as well.
				if n.Op == token.OR {
					info.inDefinition = false
				}
			}

		case *ast.ListLit:
			pushScope(closeInfo{wholeValue: true})

		case *ast.CallExpr:
			// An argument is a value of its own, like a list element.
			// The argument of close() is not: an embedded call is hoisted
			// to wrapper level along with the literal it closes.
			if !isCloseCall(n) {
				pushScope(closeInfo{wholeValue: true})
				// A definition does not close the arguments of a
				// validator such as matchN or or(), so a literal there
				// keeps its wrapper whatever reads through it.
				info.inDefinition = false
			}

		case *ast.LetClause:
			// A let value is not part of what a definition closes.
			pushScope(closeInfo{wholeValue: true})
			info.inDefinition = false

		case *ast.Alias:
			// A value alias, X={...}, binds the field's value; its literal
			// is still that whole value. A definition label closes it on
			// its own, so the literal takes no wrapper there: with one, the
			// alias, which the aliasv2 rewrite binds to self outside the
			// wrapper, no longer reaches nested fields through the builtin
			// call. Inside a comprehension the literal keeps its wrapper,
			// since the comprehension's own suspension serves the
			// conjunct widening of openCompFieldValue, which the aliased
			// literal does not take part in.
			next := closeInfo{wholeValue: true}
			if info.defLabel {
				next.suspendReclose = 1
			}
			next.aliases = append(slices.Clone(info.aliases), n)
			pushScope(next)

		case *ast.Comprehension:
			// Comprehensions are a scope boundary like conjunctions:
			// embedFlags collected inside the comprehension value must
			// not add a wrapper to the enclosing struct. No wrapper is
			// added to the comprehension value either (suspendReclose):
			// the old conditional closing cannot be expressed, as builtin
			// wrappers evaluate their argument without the enclosing
			// struct's fields, breaking self-referential guards. Opened
			// embeddings inside comprehension values are instead flagged
			// with a TODO comment.
			pushScope(closeInfo{
				suspendReclose:  1,
				inComprehension: true,
			})

		case *ast.EmbedDecl:
			info.suspendReclose++

		case *ast.StructLit:
			litStack = append(litStack, info)
			info.embedFlags = embedFlags{}
			_, sole := soleEmbed(n)
			info.wholeValue = info.wholeValue && sole
		}
		return true
	}, func(c astutil.Cursor) bool {
		switch n := c.Node().(type) {
		case *ast.Field:
			popScope(c)

			// See openCompFieldValue: comprehension conjuncts did not
			// close their fields under the old semantics, unless the
			// field had no other declaration to widen it.
			if info.inComprehension && !closedCompFields[n] && openCompFieldValue(n) {
				hasChanges = true
			}

		case *ast.BinaryExpr:
			if n.Op == token.AND || n.Op == token.OR {
				popScope(c)
			}

		case *ast.LetClause, *ast.ListLit, *ast.Alias, *ast.Comprehension:
			popScope(c)

		case *ast.CallExpr:
			if !isCloseCall(n) {
				popScope(c)
			}

		case *ast.EmbedDecl:
			info.suspendReclose--

			// Rewrite the embedding in the post-visit so that nested
			// embeddings (e.g. inside struct operands of a conjunction)
			// have already been processed; a Replace in the pre-visit
			// would prevent the children from being traversed at all.
			newExpr, exprChanged, flags := openEmbedExpr(n.Expr, info.wholeValue)
			info.embedFlags = info.embedFlags.or(flags)
			// An embedding declared directly in a comprehension value
			// lost the old conditional closing when anything in it was
			// opened, an embedding nested in a literal it embeds
			// included. The TODO says it changed meaning whatever its
			// value is, so no NOTE goes beside it.
			todo := inCompValue(c) && (exprChanged || c.Modified())
			embed := n
			if exprChanged {
				embed = newEmbed(n, newExpr, guessedSpread(flags) && !todo)
				c.Replace(embed)
				hasChanges = true
			}
			if todo {
				dropNotes(newExpr)
				ast.AddComment(embed, todoComment(
					"under the old semantics the embedding below closed the enclosing struct when the comprehension fired; it no longer does."))
			}
			if exprChanged && flags.def && len(recloseStack) == 0 {
				ast.AddComment(embed, todoComment(
					"the top-level definition embedding below is opened; if it is intended as a schema, remove its '...'."))
			}

		case *ast.StructLit:
			if openNestedClosing(n) {
				hasChanges = true
			}
			flags := info.embedFlags
			saved := litStack[len(litStack)-1]
			litStack = litStack[:len(litStack)-1]
			info.embedFlags, info.wholeValue = saved.embedFlags, saved.wholeValue
			if !info.shouldReclose() {
				// The literal cannot take a wrapper of its own, so the
				// scope which decides the wrapper needs its flags. A
				// close() hoisted out of an embedding inside the literal
				// is no longer visible to collectEmbedFlags, so the
				// closing would be lost otherwise.
				info.embedFlags = info.embedFlags.or(flags)
			} else if c.Modified() {
				hasChanges = true

				// Single embedding: { expr } ≡ expr. wholeValue keeps
				// the ... off such an embedding, so what is left here is
				// the struct argument of a hoisted close() call, or a
				// nested struct literal whose own embeddings were
				// opened. Either way its closing is now carried by the
				// flags alone, so the wrapper must restore it, applied
				// to the embedded literal directly.
				if embed, ok := singleEmbed(n); ok {
					if !flags.mayBeClosed() {
						c.ClearEnclosingModified()
						break
					}
					// Dropping the braces would orphan comments on the
					// embedding, so those keep the literal as it is.
					if s, ok := embed.Expr.(*ast.StructLit); ok && len(ast.Comments(embed)) == 0 {
						n = s
					}
				}

				// Inside a definition, a literal with a guard that reads the
				// literal's own field through an alias of an enclosing field
				// takes no wrapper: the alias becomes self outside the
				// wrapper, through which a builtin's argument cannot resolve
				// the field, and the literal would never complete. The
				// definition closes it instead, with every conjunct's fields
				// admitted, which is the one way such a struct still
				// evaluates; the guard is marked so.
				if info.inDefinition && flags.mayBeClosed() {
					if comp := guardThroughAliases(n, info.aliases); comp != nil {
						ast.AddComment(comp, todoComment(
							"the enclosing struct takes no wrapper builtin: the guard below reads the struct's own field through an alias of an enclosing field, which a wrapper's argument cannot resolve; the enclosing definition closes the struct instead, admitting the fields of every conjunct."))
						c.ClearEnclosingModified()
						break
					}
				}

				if comp := compReferringToStruct(n); comp != nil {
					ast.AddComment(comp, todoComment(
						"a wrapper builtin evaluates its argument on its own, so the guard below stays incomplete if the field it depends on is unified into the struct from elsewhere."))
				}
				// The wrapper takes the place of the literal being replaced,
				// which n is the sole embedding of when its braces were
				// dropped above.
				pos := c.Node().Pos()
				var wrapper ast.Expr = n
				switch {
				case flags.def && !flags.forceReclose:
					wrapper = wrapCall("__closeAll", n)
				case flags.other || flags.forceReclose:
					wrapper = wrapCall("__reclose", n)
					if flags.close {
						wrapper = wrapCall("close", wrapper)
					}
					// Without a definition among the embeddings, the
					// wrapper closes the struct only if one of the
					// guessed spreads inside turns out closed.
					if !flags.def {
						noteRecloseWrapper(c)
					}
				case flags.close:
					wrapper = wrapCall("close", n)
				}
				if call, ok := wrapper.(*ast.CallExpr); ok {
					ast.SetPos(call, pos)
				}
				c.Replace(wrapper)
				c.ClearEnclosingModified()
			}
		}
		return true
	}).(*ast.File), hasChanges
}

// guardsThroughAliases returns the comprehensions anywhere inside s with a
// clause that reads s, or a field of s, through one of aliases, the value
// aliases of the fields enclosing s: directly, as X.a.mode reads field mode
// of the literal at a in X's value, or through a let whose value does so.
// A guard reading through an alias a field outside s, such as a sibling's,
// does not count: the wrapper's argument resolves that field as the
// wrapper's surroundings do.
func guardsThroughAliases(s *ast.StructLit, aliases []*ast.Alias) []*ast.Comprehension {
	if len(aliases) == 0 {
		return nil
	}
	lets := map[ast.Node]bool{}
	for {
		added := false
		ast.Walk(s, func(n ast.Node) bool {
			if l, ok := n.(*ast.LetClause); ok && !lets[l] && readsInto(l.Expr, s, aliases, lets) {
				lets[l] = true
				added = true
			}
			return true
		}, nil)
		if !added {
			break
		}
	}
	var comps []*ast.Comprehension
	ast.Walk(s, func(n ast.Node) bool {
		c, ok := n.(*ast.Comprehension)
		if !ok {
			return true
		}
		for _, cl := range c.Clauses {
			if readsInto(cl, s, aliases, lets) {
				comps = append(comps, c)
				break
			}
		}
		return true
	}, nil)
	return comps
}

// guardThroughAliases is the first of [guardsThroughAliases], or nil.
func guardThroughAliases(s *ast.StructLit, aliases []*ast.Alias) *ast.Comprehension {
	if comps := guardsThroughAliases(s, aliases); len(comps) > 0 {
		return comps[0]
	}
	return nil
}

// readsInto reports whether n holds a reference to one of lets, or a
// selector path through one of aliases that reaches s.
func readsInto(n ast.Node, s *ast.StructLit, aliases []*ast.Alias, lets map[ast.Node]bool) bool {
	found := false
	ast.Walk(n, func(n ast.Node) bool {
		if found {
			return false
		}
		var labels []string
		x := n
		for {
			sel, ok := x.(*ast.SelectorExpr)
			if !ok {
				break
			}
			name, _, err := ast.LabelName(sel.Sel)
			if err != nil {
				return true
			}
			labels = append([]string{name}, labels...)
			x = sel.X
		}
		id, ok := x.(*ast.Ident)
		if !ok || id.Node == nil {
			return true
		}
		if lets[id.Node] {
			found = true
			return false
		}
		for _, a := range aliases {
			if id.Node == a && reaches(a.Expr, labels, s) {
				found = true
				return false
			}
		}
		// The path is settled; its parts need no second look.
		return false
	}, nil)
	return found
}

// reaches reports whether following labels from expr, as a selector does,
// passes through s.
func reaches(expr ast.Node, labels []string, s *ast.StructLit) bool {
	if expr == s {
		return true
	}
	switch x := expr.(type) {
	case *ast.ParenExpr:
		return reaches(x.X, labels, s)
	case *ast.UnaryExpr:
		return reaches(x.X, labels, s)
	case *ast.BinaryExpr:
		return reaches(x.X, labels, s) || reaches(x.Y, labels, s)
	case *ast.Alias:
		return reaches(x.Expr, labels, s)
	case *ast.CallExpr:
		if fn, ok := x.Fun.(*ast.Ident); ok && len(x.Args) == 1 {
			switch fn.Name {
			case "close", "__closeAll", "__reclose":
				return reaches(x.Args[0], labels, s)
			}
		}
	case *ast.StructLit:
		for _, d := range x.Elts {
			switch d := d.(type) {
			case *ast.EmbedDecl:
				if reaches(d.Expr, labels, s) {
					return true
				}
			case *ast.Comprehension:
				if reaches(d.Value, labels, s) {
					return true
				}
			case *ast.Field:
				if len(labels) == 0 {
					continue
				}
				if name, _, err := ast.LabelName(d.Label); err == nil && name == labels[0] && reaches(d.Value, labels[1:], s) {
					return true
				}
			}
		}
	}
	return false
}

// compReferringToStruct returns a comprehension among the declarations of
// s which refers to a field of s in one of its clauses, if any. A wrapper
// builtin evaluates its argument on its own, so such a guard sees the field
// without the conjuncts the struct is unified with, and does not resolve
// when the field is concrete only through them.
func compReferringToStruct(s *ast.StructLit) *ast.Comprehension {
	for _, d := range s.Elts {
		c, ok := d.(*ast.Comprehension)
		if !ok {
			continue
		}
		for _, cl := range c.Clauses {
			found := false
			ast.Walk(cl, func(n ast.Node) bool {
				if id, ok := n.(*ast.Ident); ok && id.Scope == ast.Node(s) {
					found = true
				}
				return !found
			}, nil)
			if found {
				return c
			}
		}
	}
	return nil
}

// isCloseCall reports whether call is a call to the close builtin.
func isCloseCall(call *ast.CallExpr) bool {
	id, ok := call.Fun.(*ast.Ident)
	return ok && id.Name == "close"
}

// unparen returns expr with any enclosing parentheses removed.
func unparen(expr ast.Expr) ast.Expr {
	for {
		p, ok := expr.(*ast.ParenExpr)
		if !ok {
			return expr
		}
		expr = p.X
	}
}

// oldClosedCompFields reports which fields reached through a comprehension
// body the pre-v0.18.0 semantics closed, so that [openCompFieldValue] can
// leave those alone.
//
// A conjunct inserted through a comprehension behaved like an embedding: the
// field it declared was closed by its value, except where another declaration
// widened it. Which declarations could widen it is exact: only those of the
// struct literal holding the comprehension, of the literals it embeds, and of
// its comprehension bodies. Another conjunct of the enclosing field, another
// declaration of it in the same file, and another file of the package all left
// the closing in place.
//
// One comprehension body is one conjunct, so its own declarations of a field
// intersect rather than widen each other, even when one of them has an
// ellipsis. Each body is therefore a widening group of its own, beside the
// group formed by the literal holding them and the literals it embeds.
//
// A field nested below one reached through a comprehension is decided the same
// way, one level down: the declarations which can widen it are those of its
// own label inside the values of the declarations of the enclosing field.
//
// The top level of a file is the one place where declarations the fixer cannot
// see still widen, as the files of a package share it, so nothing reached
// through a comprehension there is reported as closed.
func oldClosedCompFields(f *ast.File) map[*ast.Field]bool {
	// Struct literals which declare into an enclosing struct rather than
	// starting a struct of their own: an embedded literal and a
	// comprehension body both contribute their declarations to the literal
	// which holds them. A comprehension which is not a struct declaration,
	// such as one producing list elements, has a body which starts a struct
	// of its own, with no other declaration to widen its fields.
	inlined := make(map[*ast.StructLit]bool)
	compRoot := make(map[*ast.StructLit]bool)
	markInlined := func(decls []ast.Decl) {
		for _, d := range decls {
			switch d := d.(type) {
			case *ast.EmbedDecl:
				if s, ok := d.Expr.(*ast.StructLit); ok {
					inlined[s] = true
				}
			case *ast.Comprehension:
				if s, ok := d.Value.(*ast.StructLit); ok {
					inlined[s] = true
				}
			}
		}
	}

	// nested holds the literals which the analysis of an enclosing struct
	// descends into, as the value of one of the declarations of a field it
	// decides. They do not start a struct of their own, which would lose the
	// enclosing declarations of their path.
	nested := make(map[*ast.StructLit]bool)
	closed := make(map[*ast.Field]bool)

	// group numbers the widening groups: outerGroup for the declarations of
	// the struct itself and of the literals it embeds, and a fresh group per
	// comprehension body.
	const outerGroup = 0
	group := outerGroup

	// collect flattens the declarations which land in one struct into its
	// fields, each tagged with the group it arrives through, and reports
	// whether one of them may declare a field the fixer cannot name.
	var collect func(decls []ast.Decl, g int, fields *[]groupField) bool
	collect = func(decls []ast.Decl, g int, fields *[]groupField) (ambiguous bool) {
		for _, d := range decls {
			switch d := d.(type) {
			case *ast.Field:
				if _, _, err := ast.LabelName(d.Label); err != nil {
					// A pattern constraint or a dynamic label may
					// declare any field.
					ambiguous = true
					continue
				}
				*fields = append(*fields, groupField{d, g})
			case *ast.EmbedDecl:
				if e, ok := d.Expr.(*ast.StructLit); ok {
					ambiguous = collect(e.Elts, g, fields) || ambiguous
				} else {
					// The embedded value may declare any field.
					ambiguous = true
				}
			case *ast.Comprehension:
				if e, ok := d.Value.(*ast.StructLit); ok {
					group++
					ambiguous = collect(e.Elts, group, fields) || ambiguous
				} else {
					ambiguous = true
				}
			case *ast.Ellipsis, *ast.Alias, *ast.LetClause, *ast.Attribute,
				*ast.CommentGroup, *ast.Package, *ast.ImportDecl:
				// Declares no field of its own. An ellipsis allows
				// further fields in the struct, but does not widen
				// what any of them allows.
			default:
				ambiguous = true
			}
		}
		return ambiguous
	}

	// decide records which of the fields landing in one struct the old
	// semantics closed, and descends into the values of the declarations of
	// each field reached through a comprehension. An ambiguous struct
	// decides nothing, but the descent still claims the literals below it.
	var decide func(fields []groupField, ambiguous bool)
	decide = func(fields []groupField, ambiguous bool) {
		byName := make(map[string][]groupField)
		var names []string
		for _, gf := range fields {
			name, _, _ := ast.LabelName(gf.field.Label)
			if byName[name] == nil {
				names = append(names, name)
			}
			byName[name] = append(byName[name], gf)
		}
		for _, name := range names {
			decls := byName[name]
			viaComp := false
			for _, gf := range decls {
				if gf.group == outerGroup {
					continue
				}
				viaComp = true
				if ambiguous {
					continue
				}
				// A declaration from another group is a conjunct of its
				// own and widens the field by the fields it declares, so
				// one declaring none leaves the closing in place; one from
				// the same group is unified into this conjunct and does
				// not widen it.
				widened := slices.ContainsFunc(decls, func(other groupField) bool {
					return other.group != gf.group && declaresFields(other.field.Value)
				})
				if !widened {
					closed[gf.field] = true
				}
			}
			if !viaComp {
				// Nothing at this path came through a comprehension, so
				// a literal below it starts a struct of its own.
				continue
			}
			var sub []groupField
			subAmbiguous := ambiguous
			for _, gf := range decls {
				s, ok := gf.field.Value.(*ast.StructLit)
				if !ok {
					// The value's own declarations cannot be known.
					subAmbiguous = true
					continue
				}
				nested[s] = true
				subAmbiguous = collect(s.Elts, gf.group, &sub) || subAmbiguous
			}
			if len(sub) > 0 {
				decide(sub, subAmbiguous)
			}
		}
	}

	// The walk is in preorder, so a struct is classified by its parent
	// before it is reached, and the descent of an enclosing struct claims
	// the nested literals before the walk gets to them.
	ast.Walk(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.File:
			markInlined(n.Decls)
			// The files of a package share the top level of a file, so
			// nothing reached through a comprehension there is decided.
			var top []groupField
			collect(n.Decls, outerGroup, &top)
			decide(top, true)
		case *ast.Comprehension:
			if s, ok := n.Value.(*ast.StructLit); ok && !inlined[s] {
				compRoot[s] = true
			}
		case *ast.StructLit:
			markInlined(n.Elts)
			if inlined[n] || nested[n] {
				return true
			}
			g := outerGroup
			if compRoot[n] {
				group++
				g = group
			}
			var fields []groupField
			before := group
			ambiguous := collect(n.Elts, g, &fields)
			if g != outerGroup || group != before {
				// Something here arrived through a comprehension.
				decide(fields, ambiguous)
			}
		}
		return true
	}, nil)
	return closed
}

// groupField is one field declaration landing in a struct, paired with the
// widening group it arrives through; see [oldClosedCompFields].
type groupField struct {
	field *ast.Field
	group int
}

// declaresFields reports whether expr, as another conjunct of a closed
// field, may declare a field of its own and so widen what the field allows.
// Only a struct literal with no declarations at all cannot.
func declaresFields(expr ast.Expr) bool {
	s, ok := expr.(*ast.StructLit)
	if !ok {
		return true
	}
	for _, d := range s.Elts {
		if _, ok := d.(*ast.Attribute); !ok {
			return true
		}
	}
	return false
}

// openCompFieldValue adds a postfix ellipsis to the value of a field inside
// a comprehension when the value may resolve to a closed struct, and
// reports whether it did. Under the old semantics, conjuncts inserted
// through comprehensions were treated like embeddings and did not close
// their fields. The NOTE of a guessed spread goes on the field, where a
// comment on the value would split it from its label.
func openCompFieldValue(f *ast.Field) bool {
	// PostfixExpr is excluded: the value already has an ellipsis.
	switch f.Value.(type) {
	case *ast.Ident, *ast.SelectorExpr, *ast.IndexExpr,
		*ast.BinaryExpr, *ast.ParenExpr, *ast.CallExpr:
		if flags := collectEmbedFlags(f.Value); flags.mayBeClosed() {
			f.Value = addEllipsis(f.Value)
			if guessedSpread(flags) {
				addDeclNote(f, spreadNote)
			}
			return true
		}
	}
	return false
}

// isScalarPredeclared reports whether x is a use of a predeclared identifier
// which can never resolve to a struct, such as string or int8, so that neither
// embedding it nor a spread on it can carry closedness. An identifier which
// the parser resolved to a declaration in the file shadows the predeclared one
// and does not count.
func isScalarPredeclared(x *ast.Ident) bool {
	if x.Node != nil || x.Scope != nil {
		return false
	}
	if compile.LookupRange(x.Name) != nil {
		return true
	}
	t, ok := compile.Predeclared(x.Name).(*adt.BasicType)
	return ok && t.K&adt.StructKind == 0
}

// collectEmbedFlags recurses into an expression to collect embedding flags
// without modifying the expression. It is the single classifier of what an
// embedded expression may resolve to; [openEmbedExpr] derives its rewrites
// from the flags it returns.
func collectEmbedFlags(expr ast.Expr) embedFlags {
	switch x := expr.(type) {
	case *ast.PostfixExpr:
		// Already has ellipsis (e.g. rewritten by a nested pass);
		// still collect flags from the underlying expression.
		if x.Op == token.ELLIPSIS {
			return collectEmbedFlags(x.X)
		}
	case *ast.BinaryExpr:
		if x.Op == token.AND || x.Op == token.OR {
			xf := collectEmbedFlags(x.X)
			yf := collectEmbedFlags(x.Y)
			f := xf.or(yf)
			// A disjunction may resolve to any of its operands, so unless
			// every operand is a definition the closing has to follow the
			// branch taken: __closeAll would close a branch whose operand
			// the old semantics left open.
			if x.Op == token.OR && f.mayBeClosed() && !(xf.isDefinition() && yf.isDefinition()) {
				f.forceReclose = true
			}
			return f
		}
		// Other binary ops (e.g. +, *) cannot resolve to closed structs.
		return embedFlags{}
	case *ast.ParenExpr:
		return collectEmbedFlags(x.X)
	case *ast.Ident:
		if isTop(x) || isScalarPredeclared(x) {
			return embedFlags{}
		}
		if internal.IsDefinition(x) {
			return embedFlags{def: true}
		}
		return embedFlags{other: true}
	case *ast.SelectorExpr:
		// A definition selected from a package or a struct, or a path
		// below one, is closed recursively like a definition named
		// directly.
		if _, ok := definitionRef(x); ok {
			return embedFlags{def: true}
		}
		return embedFlags{other: true}
	case *ast.CallExpr:
		if id, ok := x.Fun.(*ast.Ident); ok {
			switch id.Name {
			case "close":
				f := embedFlags{close: true}
				if len(x.Args) == 1 {
					f = f.or(collectEmbedFlags(x.Args[0]))
				}
				return f
			case "and", "or":
				return embedFlags{other: true}
			}
		}
		return embedFlags{}
	case *ast.UnaryExpr:
		// The default marker *X takes on X's closedness; whether the
		// disjunction it marks a branch of needs a runtime check is
		// decided by the disjunction.
		if x.Op == token.MUL {
			return collectEmbedFlags(x.X)
		}
		return embedFlags{}
	case *ast.StructLit:
		// A struct literal is open by itself, but embeddings inside
		// it may still close it.
		var f embedFlags
		for _, d := range x.Elts {
			if e, ok := d.(*ast.EmbedDecl); ok {
				f = f.or(collectEmbedFlags(e.Expr))
			}
		}
		return f
	case *ast.ListLit, // Lists cannot be opened anyway (atm).
		*ast.BasicLit,
		*ast.Interpolation:
		return embedFlags{}
	}

	// Default: may resolve to a closed struct (IndexExpr, etc.)
	return embedFlags{other: true}
}

// openEmbedExpr adds postfix ellipsis to embedded expressions, classifying
// them via [collectEmbedFlags]. Conjunctions, disjunctions, and parenthesized
// expressions always get ... on the whole expression; embedded close() calls
// are hoisted; any other expression gets ... exactly when its flags indicate
// it may resolve to a closed value.
//
// whole reports that the embedding is the whole value of its struct literal
// (see [closeInfo.wholeValue]), in which case no opening is needed at all:
// nothing is declared beside the embedding for the old semantics to have
// opened it for, and an ellipsis would open the closedness of its nested
// paths too, which no wrapper restores.
func openEmbedExpr(expr ast.Expr, whole bool) (result ast.Expr, changed bool, flags embedFlags) {
	switch x := expr.(type) {
	case *ast.PostfixExpr:
		// Already has ellipsis; still collect flags from the underlying
		// expression, as they influence the wrapping of the enclosing
		// struct.
		return expr, false, collectEmbedFlags(x)

	case *ast.BinaryExpr:
		if x.Op != token.AND && x.Op != token.OR {
			// Other binary ops (e.g. +, *) don't need ellipsis.
			return expr, false, embedFlags{}
		}
		if whole {
			break
		}
		// Add ... to the entire expression rather than each operand,
		// even when no operand may resolve to a closed value.
		return addEllipsis(expr), true, collectEmbedFlags(x)

	case *ast.ParenExpr:
		if whole {
			break
		}
		// Add ... to the whole parenthesized expression.
		return addEllipsis(expr), true, collectEmbedFlags(x)

	case *ast.StructLit:
		// The literal itself needs no ellipsis — its inner embeddings
		// were already opened — but their flags still influence the
		// wrapping of the enclosing struct.
		return expr, false, collectEmbedFlags(x)

	case *ast.CallExpr:
		if isCloseCall(x) {
			// Under the old semantics, embedding close(X) closed the
			// enclosing struct while still allowing its literal
			// fields; a strict embedding of close(X) would deny them.
			// Hoist close() to wrapper level: return the processed
			// argument as the new embedding, and set the close flag
			// so the containing struct gets close() wrapping.
			if len(x.Args) == 1 {
				newArg, _, f := openCloseArg(x.Args[0], whole)
				if whole && !f.mayBeClosed() {
					// Nothing inside the argument was opened, and the
					// literal declares nothing beside the call, so the
					// call closes the literal just as it did before.
					return expr, false, embedFlags{}
				}
				f.close = true
				// The argument takes the place of the call, and keeps
				// its own comments beside those of the call.
				astutil.CopyPosition(newArg, x)
				for _, cg := range ast.Comments(x) {
					ast.AddComment(newArg, cg)
				}
				return newArg, true, f
			}
			return expr, true, embedFlags{close: true}
		}
	}

	if whole {
		// Nothing to open, and no flags to report: a wrapper on the
		// enclosing literal would close what the embedding leaves open.
		return expr, false, embedFlags{}
	}
	if f := collectEmbedFlags(expr); f.mayBeClosed() {
		return addEllipsis(expr), true, f
	}
	return expr, false, embedFlags{}
}

// openCloseArg processes the argument of an embedded close() call,
// adding ... to any embeddings inside a struct literal. For non-struct
// arguments, it returns the flags without modifying the expression
// (adding ... to a bare identifier inside close() is not valid).
//
// whole is as in [openEmbedExpr]: a close() call which is the whole value
// of its literal passes it on, since its argument is then the whole value
// of the call.
func openCloseArg(expr ast.Expr, whole bool) (ast.Expr, bool, embedFlags) {
	s, ok := expr.(*ast.StructLit)
	if !ok {
		// Non-struct argument: process like a regular embedding so that
		// e.g. close(#A) → #A... when hoisted.
		newExpr, _, f := openEmbedExpr(expr, whole)
		return newExpr, f.mayBeClosed(), f
	}
	// An embedding is the whole value of the argument literal only if
	// the literal declares nothing else, just as in the traversal.
	_, sole := soleEmbed(s)
	whole = whole && sole
	var f embedFlags
	var changed bool
	newElts := make([]ast.Decl, len(s.Elts))
	copy(newElts, s.Elts)
	for i, d := range newElts {
		embed, ok := d.(*ast.EmbedDecl)
		if !ok {
			continue
		}
		newExpr, exprChanged, ef := openEmbedExpr(embed.Expr, whole)
		f = f.or(ef)
		if exprChanged {
			changed = true
			newElts[i] = newEmbed(embed, newExpr, guessedSpread(ef))
		}
	}
	if !changed {
		return expr, false, f
	}
	newStruct := *s
	newStruct.Elts = newElts
	return &newStruct, true, f
}

// soleEmbed returns the embedding which is the only declaration of s
// contributing to its value, so that s is equivalent to it. Attributes
// declare nothing and do not count, unlike in [singleEmbed], which the
// rewrites dropping the braces of s use: those would drop the attribute.
func soleEmbed(s *ast.StructLit) (*ast.EmbedDecl, bool) {
	var found *ast.EmbedDecl
	for _, d := range s.Elts {
		switch d := d.(type) {
		case *ast.Attribute:
		case *ast.EmbedDecl:
			if found != nil {
				return nil, false
			}
			found = d
		default:
			return nil, false
		}
	}
	return found, found != nil
}

// openNestedClosing widens the closedness which an embedded struct literal
// declares for a field with the labels the enclosing literal declares for
// that same field. It reports whether it changed anything.
//
// Before v0.18.0 an embedding opened the closedness of the embedded value
// along the paths the enclosing literal declares itself, recursively, so
// that
//
//	v: {
//		close({foo?: close({foo?: _})})
//		foo: allowed: 5
//	}
//
// permitted v.foo.allowed while still denying v & {foo: other: 6}. Neither
// a spread of the embedded literal nor a wrapper around the enclosing one
// reproduces that, as a spread opens the nested closedness for every
// conjunct. Declaring the extended labels in the nested closed literal
// does: the old semantics permitted exactly the union of both field sets
// at every extended path, to any conjunct.
func openNestedClosing(lit *ast.StructLit) (changed bool) {
	// The old semantics inlined an embedded literal into the struct which
	// embeds it, so the declarations of all of them opened the closedness
	// of any value the struct embeds.
	lits := embeddedLits(lit)
	flagged := map[*ast.Comprehension]bool{}
	for i := 1; i < len(lits); i++ {
		// A literal's own declarations, and those of the literals it
		// embeds in turn, never opened its closedness: only the struct
		// which embeds it did.
		var decls []ast.Decl
		for j, t := range lits {
			if j < i || j >= i+lits[i].span {
				decls = append(decls, t.lit.Elts...)
			}
		}
		// An embedded literal is open by itself, so its own labels need
		// no widening; only the closedness of their values does.
		if widenFields(lits[i].lit, decls, false, false) {
			changed = true
		}
		// A comprehension body declares its fields only when the
		// comprehension fires, which the widening cannot express: the
		// paths it extends are opened for good, with a comment saying so.
		for _, d := range decls {
			c, ok := d.(*ast.Comprehension)
			if !ok {
				continue
			}
			if widenFields(lits[i].lit, compBody(c), false, false) {
				changed = true
				if !flagged[c] {
					flagged[c] = true
					ast.AddComment(c, todoComment(
						"the closed paths the comprehension below extends are opened whether or not it fires; the old semantics opened them only when it fired."))
				}
			}
		}
	}
	return changed
}

// compBody returns the declarations of the struct literal a comprehension
// produces, looking through nested clauses, or nil for any other value.
func compBody(c *ast.Comprehension) []ast.Decl {
	switch v := c.Value.(type) {
	case *ast.StructLit:
		return v.Elts
	case *ast.Comprehension:
		return compBody(v)
	}
	return nil
}

// embeddedLit is one literal of an embedding tree in preorder, with the
// number of entries its own subtree spans.
type embeddedLit struct {
	lit  *ast.StructLit
	span int
}

// embeddedLits returns the struct literals which the expression expr
// contributes as they are: expr itself if it is a literal, and those its
// own embeddings contribute in turn. Any other embedded expression is
// spread, which opens it recursively.
func embeddedLits(expr ast.Expr) []embeddedLit {
	s, ok := expr.(*ast.StructLit)
	if !ok {
		return nil
	}
	lits := []embeddedLit{{lit: s}}
	for _, d := range s.Elts {
		if embed, ok := d.(*ast.EmbedDecl); ok {
			lits = append(lits, embeddedLits(embed.Expr)...)
		}
	}
	lits[0].span = len(lits)
	return lits
}

// widenClosing widens the closedness which expr imposes on a struct so
// that the fields decls declares are allowed as well. It returns the
// expression to use in place of expr, which differs from it only where a
// closing had to be spread to be widened. closed says whether an
// enclosing __closeAll closes expr already.
func widenClosing(expr ast.Expr, decls []ast.Decl, closed bool) (result ast.Expr, changed bool) {
	switch x := expr.(type) {
	case *ast.ParenExpr:
		x.X, changed = widenClosing(x.X, decls, closed)
		return x, changed
	case *ast.BinaryExpr:
		// Every operand constrains the same field, so a closing in any of
		// them denies what the enclosing literal adds.
		if x.Op == token.AND || x.Op == token.OR {
			var c1, c2 bool
			x.X, c1 = widenClosing(x.X, decls, closed)
			x.Y, c2 = widenClosing(x.Y, decls, closed)
			changed = c1 || c2
		}
		return x, changed
	}
	s, recursive := closingLit(expr, closed)
	if s == nil {
		// A reference to a definition is a closing with no literal to
		// declare the added labels in, so spread it into one instead.
		if ref, ok := definitionRef(expr); ok {
			lit := &ast.StructLit{Elts: []ast.Decl{
				&ast.EmbedDecl{Expr: addEllipsis(ref)},
			}}
			if !widenFields(lit, decls, true, true) {
				return expr, false
			}
			ast.SetRelPos(lit, token.NoSpace)
			return ast.NewCall(ast.NewIdent("__closeAll"), lit), true
		}
		// An open literal denies nothing itself, but the values it
		// declares at the extended paths may.
		if lit, ok := unparen(expr).(*ast.StructLit); ok {
			return expr, widenFields(lit, decls, false, false)
		}
		return expr, false
	}
	for _, d := range s.Elts {
		// An explicit ... keeps the literal open, and close() does not
		// override it, so there is nothing to widen.
		if _, ok := d.(*ast.Ellipsis); ok {
			return expr, false
		}
	}
	return expr, widenFields(s, decls, recursive, true)
}

// definitionRef returns the definition which expr provably references: an
// identifier or a selector naming one, or a selector path below one, in
// any case possibly wrapped in struct literals which embed nothing else,
// as { X } is equivalent to X. A definition is closed, and closed
// recursively, so spreading it and closing the union again reproduces
// the old widening of it. A name is taken for a definition by its prefix
// alone; an alias or let which borrows the prefix is not told apart.
//
// Any other reference has a closedness this fix cannot know. Spreading it
// would open the closedness of its fields for every conjunct, which the
// old semantics only did along the extended paths, so such a reference is
// left as it is.
func definitionRef(expr ast.Expr) (ast.Expr, bool) {
	switch x := unparen(expr).(type) {
	case *ast.StructLit:
		if e, ok := soleEmbedExpr(x); ok {
			return definitionRef(e)
		}
	case *ast.Ident:
		if internal.IsDef(x.Name) {
			return x, true
		}
	case *ast.SelectorExpr:
		if id, ok := x.Sel.(*ast.Ident); ok && internal.IsDef(id.Name) {
			return x, true
		}
		if _, ok := definitionRef(x.X); ok {
			return x, true
		}
	}
	return nil, false
}

// widenValue returns the value to declare for a label which a closed
// value does not declare itself, so that what the enclosing literal
// declares below that label is allowed as well: a literal mirroring those
// labels, as a recursive closing closes the widening it adds too, or top
// where the enclosing literal declares no struct there at all.
func widenValue(expr ast.Expr) ast.Expr {
	decls, ok := ownDecls(expr)
	if !ok {
		return ast.NewIdent("_")
	}
	lit := &ast.StructLit{}
	widenFields(lit, decls, true, true)
	ast.SetRelPos(lit, token.NoSpace)
	return lit
}

// widenFields widens the closedness of the values which s declares for the
// fields decls extends, and declares the labels decls adds to s as
// optional fields if inject is set. recursive says whether the closedness
// of s is recursive, which makes a plain struct literal inside it closed.
func widenFields(s *ast.StructLit, decls []ast.Decl, recursive, inject bool) (changed bool) {
	for _, d := range decls {
		f, ok := d.(*ast.Field)
		if !ok {
			continue
		}
		name, ok := widenLabel(f.Label)
		if !ok {
			continue
		}
		declared := false
		for _, e := range s.Elts {
			g, ok := e.(*ast.Field)
			if !ok {
				continue
			}
			if n, ok := widenLabel(g.Label); !ok || n != name {
				continue
			}
			declared = true
			// The label is allowed already, but the closedness of its
			// value may deny what the enclosing literal adds below it.
			own, _ := ownDecls(f.Value)
			if v, c := widenClosing(g.Value, own, recursive); c {
				g.Value, changed = v, true
			}
		}
		if !declared && inject {
			s.Elts = append(s.Elts, &ast.Field{
				Label:      ast.NewStringLabel(name),
				Constraint: token.OPTION,
				Value:      widenValue(f.Value),
			})
			changed = true
		}
	}
	return changed
}

// ownDecls returns the declarations which expr contributes as a struct
// literal of its own, and whose labels the old semantics therefore allowed
// along with those of an embedded value. It reports whether expr
// contributes a struct literal at all, which an empty one does with no
// declarations of its own.
func ownDecls(expr ast.Expr) ([]ast.Decl, bool) {
	switch x := unparen(expr).(type) {
	case *ast.StructLit:
		return x.Elts, true
	case *ast.BinaryExpr:
		// A disjunct's fields are allowed only where it is selected, so
		// only a conjunction contributes all of its operands.
		if x.Op == token.AND {
			a, okA := ownDecls(x.X)
			b, okB := ownDecls(x.Y)
			return slices.Concat(a, b), okA || okB
		}
	}
	return nil, false
}

// closingLit returns the struct literal whose field set expr closes, if
// any, and whether it closes it recursively. closed says whether an
// enclosing __closeAll closes expr already.
func closingLit(expr ast.Expr, closed bool) (*ast.StructLit, bool) {
	switch x := expr.(type) {
	case *ast.StructLit:
		// A literal is open by itself; only a __closeAll around it closes
		// it, and then recursively.
		if closed {
			return x, true
		}
	case *ast.CallExpr:
		id, ok := x.Fun.(*ast.Ident)
		if !ok || len(x.Args) != 1 {
			return nil, false
		}
		switch id.Name {
		case "close", "__reclose":
			// __reclose closes conditionally, in which case widening its
			// literal is a no-op. close(__reclose(X)) is what a literal
			// with a hoisted close() and an embedding which needs a
			// runtime check gets, so unwrap either call.
			if s, ok := x.Args[0].(*ast.StructLit); ok {
				return s, false
			}
			return closingLit(x.Args[0], closed)
		case "__closeAll":
			if s, ok := x.Args[0].(*ast.StructLit); ok {
				return s, true
			}
		}
	}
	return nil, false
}

// widenLabel returns the name of l if declaring that name can widen a
// closed struct: a definition or hidden field is allowed regardless of
// closedness, and a pattern or dynamic label declares no name at all.
func widenLabel(l ast.Label) (string, bool) {
	name, _, err := ast.LabelName(l)
	if err != nil || internal.IsDefOrHidden(name) {
		return "", false
	}
	return name, true
}

// singleEmbed returns the sole element of s if it is a single embedded
// declaration.
func singleEmbed(s *ast.StructLit) (*ast.EmbedDecl, bool) {
	if len(s.Elts) != 1 {
		return nil, false
	}
	return soleEmbed(s)
}

// soleEmbedExpr returns the expression which a single-embedding literal is
// equivalent to, if dropping its braces orphans no comment on either.
func soleEmbedExpr(s *ast.StructLit) (ast.Expr, bool) {
	e, ok := singleEmbed(s)
	if !ok || len(ast.Comments(s)) != 0 || len(ast.Comments(e)) != 0 {
		return nil, false
	}
	return e.Expr, true
}

func addEllipsis(expr ast.Expr) *ast.PostfixExpr {
	return &ast.PostfixExpr{
		X:     expr,
		Op:    token.ELLIPSIS,
		OpPos: expr.End(),
	}
}

// spreadNote is the NOTE of a spread the rewrite cannot tell is needed;
// see [guessedSpread].
const spreadNote = "the spread operator below may not be needed: it only matters if the value is a closed struct, or closes a nested struct at a path which the enclosing struct also declares."

// guessedSpread reports whether the flags f, collected from an expression
// being spread, leave it unknown whether the spread is needed: f proves
// the value closed only through a definition or a close() call. Any
// other value is a reference the fixer cannot resolve, which in practice
// usually names an open struct, where the spread changes nothing.
func guessedSpread(f embedFlags) bool {
	return !f.def && !f.close
}

// newEmbed returns the embedding which replaces old once its expression
// is rewritten to expr, with old's comments and, if note is set, the NOTE
// of a guessed spread.
func newEmbed(old *ast.EmbedDecl, expr ast.Expr, note bool) *ast.EmbedDecl {
	e := &ast.EmbedDecl{Expr: expr}
	astutil.CopyComments(e, old)
	if note {
		addDeclNote(e, spreadNote)
	}
	return e
}

// addDeclNote adds a NOTE with message msg as a doc comment of d, on the
// line before it, taking d's own layout position so that a blank line
// before d stays before the NOTE. A NOTE d already carries is not
// repeated.
func addDeclNote(d ast.Decl, msg string) {
	cg := noteComment(msg)
	for _, have := range ast.Comments(d) {
		if len(have.List) > 0 && have.List[0].Text == cg.List[0].Text {
			return
		}
	}
	ast.SetRelPos(cg, d.Pos().RelPos())
	ast.AddComment(d, cg)
}

// inCompValue reports whether the embedding at c is declared directly in
// the value of a comprehension, rather than in a literal nested there.
func inCompValue(c astutil.Cursor) bool {
	lit := c.Parent()
	if lit == nil || lit.Parent() == nil {
		return false
	}
	comp, ok := lit.Parent().Node().(*ast.Comprehension)
	return ok && comp.Value == lit.Node()
}

// noteRecloseWrapper marks the __reclose wrapper replacing the literal at
// c as one the rewrite cannot tell is needed, on the line before it: as a
// doc comment on the nearest enclosing declaration, since on the wrapper
// itself it would split a field from its label.
func noteRecloseWrapper(c astutil.Cursor) {
	for p := c.Parent(); p != nil; p = p.Parent() {
		switch d := p.Node().(type) {
		case *ast.Field, *ast.EmbedDecl, *ast.LetClause:
			addDeclNote(d.(ast.Decl), "the __reclose wrapper below may not be needed: it only closes the struct if one of the spread values in it is a closed struct.")
			return
		}
	}
}

// dropNotes removes the spread NOTEs of the embeddings anywhere inside n.
func dropNotes(n ast.Node) {
	first := noteComment(spreadNote).List[0].Text
	isNote := func(cg *ast.CommentGroup) bool {
		return len(cg.List) > 0 && cg.List[0].Text == first
	}
	ast.Walk(n, func(n ast.Node) bool {
		if e, ok := n.(*ast.EmbedDecl); ok {
			if cgs := ast.Comments(e); slices.ContainsFunc(cgs, isNote) {
				ast.SetComments(e, slices.DeleteFunc(slices.Clone(cgs), isNote))
			}
		}
		return true
	}, nil)
}
