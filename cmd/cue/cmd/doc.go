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
	"cmp"
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/filetypes"
	"cuelang.org/go/internal/golangorgx/gopls/protocol"
	"cuelang.org/go/internal/lsp/fscache"
	"cuelang.org/go/internal/source"
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

	fs := fscache.NewCUECachedFS()
	now := time.Now()

	for _, inst := range b.insts {
		if len(inst.BuildFiles) == 0 {
			continue
		}
		fs := fscache.NewOverlayFS(fs)

		parserCfg := parser.NewConfig()
		modFile := inst.ModuleFile
		if modFile == nil {
			continue
		}
		if modFile.Language != nil {
			versionOption := parser.Version(modFile.Language.Version)
			parserCfg = parser.NewConfig(versionOption)
		}

		asts := make([]*ast.File, len(inst.BuildFiles))
		err := fs.Update(func(txn *fscache.UpdateTxn) error {
			for i, file := range inst.BuildFiles {
				src, err := source.ReadAll(file.Filename, file.Source)
				if err != nil {
					return err
				}
				uri := protocol.URIFromPath(file.Filename)
				fh, err := txn.Set(uri, src, now, 0)
				if err != nil {
					return err
				}
				syntax, _, err := fh.ReadCUE(parserCfg)
				if syntax == nil {
					return err
				}
				asts[i] = syntax
			}
			return nil
		})
		if err != nil {
			return err
		}

		ip := ast.ImportPath{Qualifier: asts[0].PackageName()}
		modPath, version, _ := ast.SplitPackageVersion(modFile.QualifiedModule())

		pkgPath, err := filepath.Rel(inst.Root, inst.Dir)
		if err != nil {
			return err
		}
		ip.Path = path.Clean(modPath + "/" + filepath.ToSlash(pkgPath))
		ip.Version = version
		ip = ip.Canonical()

		var sb strings.Builder
		fmt.Fprintf(&sb, "Docs for pkg %v\n", ip)

		evalCfg := eval.Config{IP: ip}
		e := eval.New(evalCfg, asts...)

		// The package clauses carry the package documentation.
		writeDocComments(&sb, 0, docCommentsOf(eval.NodeSet{e.PackageClauses()}))

		root := e.Root()
		seen := map[*eval.Node]bool{root: true}
		writeFields(&sb, 0, eval.NodeSet{root}, seen)

		fmt.Fprint(cmd.OutOrStdout(), sb.String())
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
// Fields declared by exactly one branch of a disjunction are grouped
// by branch, under "one of:"/"or:" headers, and a branch that is a
// reference gets its "also includes" link attributed to it. Fields
// declared outside any branch — or by several branches, in which
// case they are present whichever branch is taken — are documented
// before the branches.
//
// Fields whose names begin with "_" (hidden fields) are not
// documented. There is no depth limit: recursion continues until a
// field has no public children.
func writeFields(sb *strings.Builder, depth int, current eval.NodeSet, seen map[*eval.Node]bool) {
	indent := strings.Repeat(docIndent, depth)

	docSet := slices.Clone(current)
	var expansion eval.NodeSet
	for _, member := range current.Expand() {
		switch _, addressable := member.FieldPath(); {
		case slices.Contains(current, member):
			// Already part of the set being documented.
		case addressable:
			expansion = append(expansion, member)
		case seen[member]:
			// A reference cycle through unaddressable nodes: the
			// member is already documented (or being documented)
			// further up.
		default:
			seen[member] = true
			docSet = append(docSet, member)
		}
	}

	// Identify the leaf branches of any disjunctions declaring this
	// node, and the nodes that reference-branches resolve to.
	branches := disjunctionBranches(docSet)
	branchLinks := make([][]string, len(branches))
	branchTarget := make(map[*eval.Node]bool)
	for i, branch := range branches {
		v := branch.Value()
		for {
			// A default branch: the reference, if any, is within.
			u, ok := v.(*ast.UnaryExpr)
			if !ok || u.Op != token.MUL {
				break
			}
			v = u.X
		}
		for _, target := range branch.Resolve(v) {
			if path, addressable := target.FieldPath(); addressable {
				branchTarget[target] = true
				branchLinks[i] = append(branchLinks[i], strings.Join(path, "."))
			}
		}
	}

	// Addressable expansion members are documented at their own
	// paths: only link to them, attributing the link to a branch
	// where one is responsible.
	var links []string
	for _, member := range expansion {
		if branchTarget[member] {
			continue
		}
		path, _ := member.FieldPath()
		links = append(links, strings.Join(path, "."))
	}
	slices.Sort(links)
	links = slices.Compact(links)
	for _, link := range links {
		fmt.Fprintf(sb, "%salso includes %s\n", indent, link)
	}

	// declaredBy records which declarations declare each field node,
	// and branchOf attributes a field to a branch iff every one of
	// its declarations lies within that single branch.
	declaredBy := make(map[*eval.Node][]eval.Decl)
	for d := range docSet.Decls() {
		for _, child := range d.Fields() {
			declaredBy[child] = append(declaredBy[child], d)
		}
	}
	branchOf := func(m *eval.Node) int {
		branch := -1
		for _, d := range declaredBy[m] {
			b := containingBranch(branches, d)
			if b == -1 || (branch != -1 && branch != b) {
				return -1
			}
			branch = b
		}
		return branch
	}

	type fieldEntry struct {
		name string
		here eval.NodeSet
	}
	var common []fieldEntry
	branchFields := make([][]fieldEntry, len(branches))
	for name, members := range docSet.Fields() {
		if strings.HasPrefix(name, "_") {
			continue
		}
		byBranch := make(map[int]eval.NodeSet)
		for _, member := range members {
			if seen[member] {
				continue
			}
			seen[member] = true
			b := branchOf(member)
			byBranch[b] = append(byBranch[b], member)
		}
		if here := byBranch[-1]; len(here) > 0 {
			common = append(common, fieldEntry{name, here})
		}
		for i := range branches {
			if here := byBranch[i]; len(here) > 0 {
				branchFields[i] = append(branchFields[i], fieldEntry{name, here})
			}
		}
	}

	for _, fe := range common {
		writeFieldBlock(sb, depth, fe.name, fe.here, seen)
	}

	// Skip the branch headers entirely when no branch has anything
	// to document (e.g. a disjunction of scalars).
	showBranches := false
	for i := range branches {
		if len(branchFields[i]) > 0 || len(branchLinks[i]) > 0 {
			showBranches = true
		}
	}
	if !showBranches {
		return
	}
	for i := range branches {
		header := "or:"
		if i == 0 {
			header = "one of:"
		}
		fmt.Fprintf(sb, "%s%s\n", indent, header)
		for _, link := range branchLinks[i] {
			fmt.Fprintf(sb, "%s%salso includes %s\n", indent, docIndent, link)
		}
		for _, fe := range branchFields[i] {
			writeFieldBlock(sb, depth+1, fe.name, fe.here, seen)
		}
	}
}

// writeFieldBlock documents a single field: its name and optionality,
// its doc comments, and, recursively, its contents.
func writeFieldBlock(sb *strings.Builder, depth int, name string, here eval.NodeSet, seen map[*eval.Node]bool) {
	indent := strings.Repeat(docIndent, depth)
	fmt.Fprintf(sb, "%s%s%s:\n", indent, name, constraintMarker(here))
	writeDocComments(sb, depth+1, docCommentsOf(here))
	writeFields(sb, depth+1, here, seen)
}

// disjunctionBranches returns the declarations that are leaf branches
// of the disjunctions declaring the given nodes, in source order. A
// nested disjunction (a | b | c) contributes one [eval.DeclDisjunct]
// per interior binary expression as well as one per operand; only the
// operands — the actual alternatives — are returned.
func disjunctionBranches(nodes eval.NodeSet) []eval.Decl {
	var branches []eval.Decl
	for d := range nodes.Decls() {
		if d.Kind() != eval.DeclDisjunct {
			continue
		}
		if be, ok := d.Value().(*ast.BinaryExpr); ok && be.Op == token.OR {
			continue
		}
		branches = append(branches, d)
	}
	slices.SortFunc(branches, func(a, b eval.Decl) int {
		aPos, bPos := a.Value().Pos().Position(), b.Value().Pos().Position()
		return cmp.Or(
			cmp.Compare(aPos.Filename, bPos.Filename),
			cmp.Compare(aPos.Offset, bPos.Offset),
		)
	})
	return branches
}

// containingBranch returns the index of the branch whose value
// syntactically contains d's value, or -1 if there is none. This is
// what attributes the declarations within a branch — including
// conjunction operands, defaults, and comprehension bodies nested
// inside it — to that branch.
func containingBranch(branches []eval.Decl, d eval.Decl) int {
	v := d.Value()
	if v == nil || !v.Pos().IsValid() {
		return -1
	}
	for i, branch := range branches {
		bv := branch.Value()
		if bv.Pos().File() != v.Pos().File() {
			continue
		}
		if bv.Pos().Offset() <= v.Pos().Offset() && v.End().Offset() <= bv.End().Offset() {
			return i
		}
	}
	return -1
}

// constraintMarker renders the optionality of the field constituted
// by the given nodes, merging the markers of its declarations with
// CUE's unification semantics: any regular declaration makes the
// field regular, otherwise a required declaration wins over optional
// ones.
func constraintMarker(nodes eval.NodeSet) string {
	var optional, required bool
	for d := range nodes.Decls() {
		switch d.Constraint() {
		case token.OPTION:
			optional = true
		case token.NOT:
			required = true
		default:
			if d.Kind() == eval.DeclField {
				return ""
			}
		}
	}
	switch {
	case required:
		return "!"
	case optional:
		return "?"
	}
	return ""
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
		aPos, bPos := a.Pos().Position(), b.Pos().Position()
		return cmp.Or(
			cmp.Compare(aPos.Filename, bPos.Filename),
			cmp.Compare(aPos.Offset, bPos.Offset),
		)
	})
	return comments
}

// writeDocComments writes the text of the given comment groups at the
// given indentation depth.
func writeDocComments(sb *strings.Builder, depth int, comments []*ast.CommentGroup) {
	indent := strings.Repeat(docIndent, depth)
	for _, cg := range comments {
		text := strings.TrimRight(cg.Text(), "\n")
		if text == "" {
			continue
		}
		for line := range strings.Lines(text) {
			sb.WriteString(indent)
			sb.WriteString(strings.TrimRight(line, "\n"))
			sb.WriteString("\n")
		}
	}
}
