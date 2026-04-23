// Copyright 2026 The CUE Authors
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

package cmd

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/filetypes"
	"cuelang.org/go/unstable/lsp/eval"
	"github.com/spf13/cobra"
)

// newDocCmd creates the doc command
func newDocCmd(c *Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "doc",
		Short: "docs for a pkg",
		RunE:  mkRunE(c, runDoc),
	}

	addOrphanFlags(cmd)
	addInjectionFlags(cmd)

	return cmd
}

func runDoc(cmd *Command, args []string) error {
	b, err := parseArgs(cmd, args, &config{mode: filetypes.Input})
	if err != nil {
		return err
	}

	w := cmd.OutOrStdout()
	for _, inst := range b.insts {
		if len(inst.BuildFiles) == 0 || inst.ModuleFile == nil {
			continue
		}
		opts := []parser.Option{parser.ParseComments}
		if lang := inst.ModuleFile.Language; lang != nil {
			opts = append(opts, parser.Version(lang.Version))
		}

		asts := make([]*ast.File, len(inst.BuildFiles))
		for i, file := range inst.BuildFiles {
			syntax, err := parser.ParseFile(file.Filename, file.Source, opts...)
			if err != nil {
				return err
			}
			asts[i] = syntax
		}

		ip := ast.ParseImportPath(inst.ImportPath).Canonical()
		fmt.Fprintf(w, "Docs for pkg %v\n", ip)

		e := eval.New(eval.Config{IP: ip}, asts...)

		// The package clauses carry the package documentation.
		writeDocComments(w, 0, docCommentsOf(eval.NodeSet{e.PackageClauses()}))

		root := e.Root()
		seen := map[*eval.Node]bool{root: true}
		writeFields(w, 0, eval.NodeSet{root}, seen)
	}

	return nil
}

// docIndent is the indentation applied per level of field nesting.
const docIndent = "    "

// writeFields documents the public fields of current: a set of nodes
// that jointly constitute the field (or package root) being
// documented. seen holds every node documented so far, guarding
// against reference cycles; callers must pre-mark the members of
// current.
//
// The set's expansion determines what is documented where. Expansion
// members that are addressable elsewhere (they have a field path of
// their own) are documented at their own paths, and are mentioned
// here with a single "also includes" link — rather than enumerating
// every field they contribute. Unaddressable members (e.g. inline
// expressions, whose field path is nil) join the set being
// documented here, since their fields can be documented nowhere
// else.
//
// Disjunctions are discovered by walking the syntax of the node's
// own declarations, which holds the authoritative branch structure:
// chains flatten (a | b | c has three branches), parentheses and
// default markers are seen through, and each independent disjunction
// gets its own group of "one of:"/"or:" headers. Fields declared by
// exactly one branch are grouped under that branch, and a branch
// that is a reference gets its "also includes" link attributed to
// it. Fields declared outside any branch — or by several branches,
// in which case they are present whichever branch is taken — are
// documented before the disjunctions.
//
// Pattern constraints and dynamic fields are documented like fields,
// headed by their rendered labels (e.g. [string] or "\(expr)")
// instead of names, after the named fields of their section; they
// too are attributed to disjunction branches.
//
// Fields whose names begin with "_" (hidden fields) are not
// documented. There is no depth limit: recursion continues until a
// field has no public children.
func writeFields(w io.Writer, depth int, current eval.NodeSet, seen map[*eval.Node]bool) {
	indent := strings.Repeat(docIndent, depth)

	docSet, linkTargets := splitExpansion(current, seen)
	disjunctions := findDisjunctions(docSet)

	// exprBranch maps every AST node within a branch expression to
	// its branch. Declarations anchor into it below by node identity
	// of their keys and labels: having walked each branch's subtree,
	// we know natively which declarations it contains, with no need
	// for position arithmetic.
	type branchID struct{ dis, br int }
	exprBranch := make(map[ast.Node]branchID)
	for di, dj := range disjunctions {
		for bi, expr := range dj.branches {
			ast.Walk(expr, func(n ast.Node) bool {
				exprBranch[n] = branchID{di, bi}
				return true
			}, nil)
		}
	}

	// A branch that is a reference resolves to the nodes it includes;
	// its "also includes" links are attributed to the branch.
	branchLinks := make(map[branchID][]string)
	branchTarget := make(map[*eval.Node]bool)
	for di, dj := range disjunctions {
		for bi, expr := range dj.branches {
			id := branchID{di, bi}
			for _, target := range dj.decl.Resolve(expr) {
				if path, addressable := target.FieldPath(); addressable {
					branchTarget[target] = true
					branchLinks[id] = append(branchLinks[id], renderFieldPath(path))
				}
			}
			slices.Sort(branchLinks[id])
			branchLinks[id] = slices.Compact(branchLinks[id])
		}
	}

	// Addressable expansion members are documented at their own
	// paths: only link to them, attributing the link to a branch
	// where one is responsible.
	var links []string
	for _, lt := range linkTargets {
		if branchTarget[lt.node] {
			continue
		}
		links = append(links, lt.path)
	}
	slices.Sort(links)
	links = slices.Compact(links)
	for _, link := range links {
		fmt.Fprintf(w, "%salso includes %s\n", indent, link)
	}

	// branchOf attributes a node to a branch iff every one of its
	// declarations is anchored within that single branch.
	branchOf := func(m *eval.Node) (branchID, bool) {
		var id branchID
		found := false
		for d := range m.Decls() {
			anchor := d.Key()
			if anchor == nil {
				anchor = d.Value()
			}
			if anchor == nil {
				continue
			}
			b, ok := exprBranch[anchor]
			if !ok || (found && b != id) {
				return branchID{}, false
			}
			id, found = b, true
		}
		return id, found
	}

	type fieldEntry struct {
		name string
		here eval.NodeSet
	}
	var common []fieldEntry
	branchFields := make(map[branchID][]fieldEntry)
	for name, members := range docSet.Fields() {
		if strings.HasPrefix(name, "_") {
			continue
		}
		// Render the field's heading as a CUE selector, quoting names
		// that need it.
		heading := renderFieldPath([]string{name})
		var commonHere eval.NodeSet
		byBranch := make(map[branchID]eval.NodeSet)
		for _, member := range members {
			if seen[member] {
				continue
			}
			seen[member] = true
			if id, ok := branchOf(member); ok {
				byBranch[id] = append(byBranch[id], member)
			} else {
				commonHere = append(commonHere, member)
			}
		}
		if len(commonHere) > 0 {
			common = append(common, fieldEntry{heading, commonHere})
		}
		for di := range disjunctions {
			for bi := range disjunctions[di].branches {
				id := branchID{di, bi}
				if here := byBranch[id]; len(here) > 0 {
					branchFields[id] = append(branchFields[id], fieldEntry{heading, here})
				}
			}
		}
	}

	// Pattern constraints and dynamic fields are anonymous, and
	// headed by their labels instead of names.
	appendLabeled := func(node *eval.Node) {
		if seen[node] {
			return
		}
		seen[node] = true
		for d := range node.Decls() {
			entry := fieldEntry{renderDocLabel(d.Label()), eval.NodeSet{node}}
			if id, ok := branchOf(node); ok {
				branchFields[id] = append(branchFields[id], entry)
			} else {
				common = append(common, entry)
			}
			break
		}
	}
	for _, member := range docSet {
		for _, pattern := range member.Patterns() {
			appendLabeled(pattern)
		}
		for _, dynamic := range member.Dynamics() {
			appendLabeled(dynamic)
		}
	}

	for _, fe := range common {
		writeFieldBlock(w, depth, fe.name, fe.here, seen)
	}

	for di, dj := range disjunctions {
		// Skip a disjunction's headers entirely when none of its
		// branches has anything to document (e.g. a disjunction of
		// scalars).
		hasContent := false
		for bi := range dj.branches {
			id := branchID{di, bi}
			if len(branchFields[id]) > 0 || len(branchLinks[id]) > 0 {
				hasContent = true
				break
			}
		}
		if !hasContent {
			continue
		}
		for bi := range dj.branches {
			id := branchID{di, bi}
			header := "or:"
			if bi == 0 {
				header = "one of:"
			}
			fmt.Fprintf(w, "%s%s\n", indent, header)
			for _, link := range branchLinks[id] {
				fmt.Fprintf(w, "%s%salso includes %s\n", indent, docIndent, link)
			}
			for _, fe := range branchFields[id] {
				writeFieldBlock(w, depth+1, fe.name, fe.here, seen)
			}
		}
	}
}

// A linkTarget is an expansion member that is documented at its own
// path elsewhere, to be mentioned with an "also includes" link.
type linkTarget struct {
	node *eval.Node
	path string
}

// splitExpansion partitions the expansion of current: unaddressable
// members join the returned docSet, since their fields can be
// documented nowhere else (they are marked seen), whereas addressable
// members are returned as link targets.
func splitExpansion(current eval.NodeSet, seen map[*eval.Node]bool) (docSet eval.NodeSet, targets []linkTarget) {
	docSet = slices.Clone(current)
	inCurrent := make(map[*eval.Node]bool, len(current))
	for _, member := range current {
		inCurrent[member] = true
	}
	// A NodeSet's member order is unspecified: impose source-position
	// order at this boundary, so that everything built from docSet
	// downstream — disjunction-group order, pattern and dynamic-field
	// entries, nested field sets — renders deterministically.
	expanded := current.Expand()
	slices.SortFunc(expanded, func(a, b *eval.Node) int {
		return nodeDocPos(a).Compare(nodeDocPos(b))
	})
	for _, member := range expanded {
		switch path, addressable := member.FieldPath(); {
		case inCurrent[member]:
			// Already part of the set being documented.
		case addressable:
			targets = append(targets, linkTarget{member, renderFieldPath(path)})
		case seen[member]:
			// A reference cycle through unaddressable nodes: the
			// member is already documented (or being documented)
			// further up.
		default:
			seen[member] = true
			docSet = append(docSet, member)
		}
	}
	return docSet, targets
}

// nodeDocPos returns the earliest source position of any of the
// node's declarations, or [token.NoPos] (which sorts after all valid
// positions) if none has one.
func nodeDocPos(n *eval.Node) token.Pos {
	pos := token.NoPos
	for d := range n.Decls() {
		v := d.Value()
		if v == nil {
			// e.g. a package clause, or a bare ellipsis.
			continue
		}
		if p := v.Pos(); p.HasAbsPos() && p.Compare(pos) < 0 {
			pos = p
		}
	}
	return pos
}

// A docDisjunction is one disjunction expression contributing to the
// value of the node being documented.
type docDisjunction struct {
	// decl is the declaration whose value contains the disjunction.
	decl *eval.Decl
	// branches holds one expression per alternative, in source
	// order, with parentheses and default markers stripped.
	branches []ast.Expr
}

// findDisjunctions discovers the disjunctions contributing to the
// value of the node constituted by docSet, by walking the syntax of
// its declarations — which holds the authoritative branch structure.
// Decls of the derived kinds (conjunction and disjunction operands,
// defaults) are sub-expressions of a sibling decl's value, and are
// skipped so that each disjunction is found exactly once, in its
// outermost form.
func findDisjunctions(docSet eval.NodeSet) []docDisjunction {
	var result []docDisjunction
	for d := range docSet.Decls() {
		switch d.Kind() {
		case eval.DeclConjunct, eval.DeclDisjunct, eval.DeclDefault:
			continue
		}
		// Files, package clauses, and other value-less decls do not
		// hold an expression.
		if v, ok := d.Value().(ast.Expr); ok {
			result = appendDisjunctions(result, d, v)
		}
	}
	return result
}

// appendDisjunctions appends the disjunctions found in the operator
// structure of expr: a disjunction chain at the top of expr itself,
// or nested within conjunctions, parentheses, or defaults. It does
// not descend into struct literals: their fields' values belong to
// child nodes, and expressions embedded within them are separate
// declarations, walked in their own right.
func appendDisjunctions(result []docDisjunction, d *eval.Decl, expr ast.Expr) []docDisjunction {
	switch e := expr.(type) {
	case *ast.ParenExpr:
		return appendDisjunctions(result, d, e.X)
	case *ast.UnaryExpr:
		if e.Op == token.MUL {
			return appendDisjunctions(result, d, e.X)
		}
	case *ast.BinaryExpr:
		switch e.Op {
		case token.OR:
			return append(result, docDisjunction{d, appendBranches(nil, e)})
		case token.AND:
			result = appendDisjunctions(result, d, e.X)
			result = appendDisjunctions(result, d, e.Y)
		}
	}
	return result
}

// appendBranches flattens the disjunction chain rooted at expr into
// its branches, in source order, seeing through parentheses and
// default markers.
func appendBranches(branches []ast.Expr, expr ast.Expr) []ast.Expr {
	expr = stripBranchExpr(expr)
	if be, ok := expr.(*ast.BinaryExpr); ok && be.Op == token.OR {
		branches = appendBranches(branches, be.X)
		return appendBranches(branches, be.Y)
	}
	return append(branches, expr)
}

// stripBranchExpr strips parentheses and unary * default markers
// from a branch expression.
func stripBranchExpr(expr ast.Expr) ast.Expr {
	for {
		switch e := expr.(type) {
		case *ast.ParenExpr:
			expr = e.X
		case *ast.UnaryExpr:
			if e.Op != token.MUL {
				return expr
			}
			expr = e.X
		default:
			return expr
		}
	}
}

// writeFieldBlock documents a single field: its name and optionality,
// its doc comments, and, recursively, its contents.
func writeFieldBlock(w io.Writer, depth int, name string, here eval.NodeSet, seen map[*eval.Node]bool) {
	indent := strings.Repeat(docIndent, depth)
	fmt.Fprintf(w, "%s%s%s:\n", indent, name, constraintMarker(here))
	writeDocComments(w, depth+1, docCommentsOf(here))
	writeFields(w, depth+1, here, seen)
}

// constraintMarker renders the optionality of the field constituted
// by the given nodes. Unifying the members' constraints is valid
// here because a documented field's members are same-named fields
// merged by struct unification — the precondition that
// [eval.Node.Constraint] requires of such a fold.
func constraintMarker(nodes eval.NodeSet) string {
	// The fold starts from optional, the weakest constraint in the
	// subsumption order.
	constraint, found := eval.ConstraintOptional, false
	for _, n := range nodes {
		if c, ok := n.Constraint(); ok {
			constraint, found = constraint.UnifyConstraints(c), true
		}
	}
	if !found {
		return ""
	}
	switch constraint {
	case eval.ConstraintOptional:
		return "?"
	case eval.ConstraintRequired:
		return "!"
	}
	return ""
}

// renderFieldPath renders a field path as CUE selectors, quoting
// field names that need it, e.g. x."we-ird".y.
func renderFieldPath(path []string) string {
	selectors := make([]cue.Selector, len(path))
	for i, name := range path {
		if strings.HasPrefix(name, "#") {
			selectors[i] = cue.Def(name)
		} else {
			selectors[i] = cue.Str(name)
		}
	}
	return cue.MakePath(selectors...).String()
}

// renderDocLabel renders the label of a pattern constraint or
// dynamic field, e.g. [string] or ("foo"+"bar") or "\(expr)".
func renderDocLabel(label ast.Label) string {
	if label == nil {
		return "?"
	}
	b, err := format.Node(label)
	if err != nil {
		return "?"
	}
	return strings.TrimSpace(string(b))
}

// docCommentsOf collects the doc comments of the nodes' own
// declarations, in source order. It deliberately does not expand the
// set: documentation is not inherited through references.
func docCommentsOf(nodes eval.NodeSet) []*ast.CommentGroup {
	var comments []*ast.CommentGroup
	for d := range nodes.Decls() {
		comments = append(comments, d.DocComments()...)
	}
	slices.SortFunc(comments, func(a, b *ast.CommentGroup) int {
		return a.Pos().Compare(b.Pos())
	})
	return comments
}

// writeDocComments writes the text of the given comment groups at the
// given indentation depth.
func writeDocComments(w io.Writer, depth int, comments []*ast.CommentGroup) {
	indent := strings.Repeat(docIndent, depth)
	for _, cg := range comments {
		text := strings.TrimRight(cg.Text(), "\n")
		if text == "" {
			continue
		}
		for line := range strings.Lines(text) {
			fmt.Fprintf(w, "%s%s\n", indent, strings.TrimRight(line, "\n"))
		}
	}
}
