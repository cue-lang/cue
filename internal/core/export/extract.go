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
	"slices"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
)

// ExtractDoc collects documentation strings for a field.
//
// Comments are attached to a field with a field shorthand belong to the
// child node. So in the following the comment is attached to field bar.
//
//	// comment
//	foo: bar: 2
func ExtractDoc(v *adt.Vertex) (docs []*ast.CommentGroup) {
	return extractDocs(v)
}

func extractDocs(v *adt.Vertex) (docs []*ast.CommentGroup) {
	for x := range v.LeafConjuncts() {
		// TODO: Is this still being used?
		if v, ok := x.Elem().(*adt.Vertex); ok {
			docs = append(docs, extractDocs(v)...)
		}

		switch f := x.Field().Source().(type) {
		case *ast.Field:
			// ast.DocComments resolves the field-chain convention from
			// the (parse-resolved) source AST, so a doc syntactically
			// attached to a field-chain head is reported on the leaf
			// field it documents. This works regardless of whether v has
			// a parent, which is why it is also correct for the
			// synthetic vertices built in the expr export path.
			for _, cg := range ast.DocComments(f) {
				if !containsDoc(docs, cg) {
					docs = append(docs, cg)
				}
			}
			// DocComments returns only comments that document the field
			// (Position 0). A field can also carry trailing or dangling
			// doc comments (e.g. a comment at the end of a struct); those
			// do not document the field, but we carry them through so the
			// exported form preserves them in place.
			for _, cg := range ast.Comments(f) {
				if cg.Doc && cg.Position != 0 && !containsDoc(docs, cg) {
					docs = append(docs, cg)
				}
			}

		case *ast.File:
			fdocs, _ := internal.FileComments(f)
			docs = append(docs, fdocs...)
		}
	}
	return docs
}

// leadingDocComments returns the doc-position comment groups in cgs,
// in order. When non-doc comment groups that the exporter discards
// precede the first doc comment, that first doc comment inherits the
// leading RelPos of cgs[0].
func leadingDocComments(cgs []*ast.CommentGroup) []*ast.CommentGroup {
	var docs []*ast.CommentGroup
	for i, cg := range cgs {
		if cg.Doc {
			if i > 0 && len(docs) == 0 {
				cg = withLeadingRelPos(cg, cgs[0].Pos().RelPos())
			}
			docs = append(docs, cg)
		}
	}
	return docs
}

// withLeadingRelPos returns cg with the RelPos of its first comment's
// slash set to rel. It clones cg so the input is unaltered. cg is
// returned unchanged when it is empty or already carries rel.
func withLeadingRelPos(cg *ast.CommentGroup, rel token.RelPos) *ast.CommentGroup {
	if cg == nil || len(cg.List) == 0 || cg.Pos().RelPos() == rel {
		return cg
	}
	clone := *cg
	clone.List = slices.Clone(cg.List)
	first := clone.List[0]
	first.Slash = first.Slash.WithRel(rel)
	return &clone
}

func containsDoc(a []*ast.CommentGroup, cg *ast.CommentGroup) bool {
	if slices.Contains(a, cg) {
		return true
	}

	for _, c := range a {
		if c.Text() == cg.Text() {
			return true
		}
	}

	return false
}

func ExtractFieldAttrs(v *adt.Vertex) (attrs []*ast.Attribute) {
	for x := range v.LeafConjuncts() {
		attrs = extractFieldAttrs(attrs, x.Field())
	}
	return attrs
}

// extractFieldAttrs extracts the fields from n and appends unique entries to
// attrs.
//
// The value of n should be obtained from the Conjunct.Field method if the
// source for n is a Conjunct so that Comprehensions are properly unwrapped.
func extractFieldAttrs(attrs []*ast.Attribute, n adt.Node) []*ast.Attribute {
	if f, ok := n.Source().(*ast.Field); ok {
		for _, a := range f.Attrs {
			if !containsAttr(attrs, a) {
				attrs = append(attrs, a)
			}
		}
	}
	return attrs
}

func ExtractDeclAttrs(v *adt.Vertex) (attrs []*ast.Attribute) {
	for _, st := range v.Structs {
		if src := st.StructLit; src != nil {
			attrs = extractDeclAttrs(attrs, src.Src)
		}
	}
	return attrs
}

func extractDeclAttrs(attrs []*ast.Attribute, n ast.Node) []*ast.Attribute {
	switch x := n.(type) {
	case nil:
	case *ast.File:
		attrs = appendDeclAttrs(attrs, x.Decls[len(x.Preamble()):])
	case *ast.StructLit:
		attrs = appendDeclAttrs(attrs, x.Elts)
	}
	return attrs
}

func appendDeclAttrs(a []*ast.Attribute, decls []ast.Decl) []*ast.Attribute {
	for _, d := range decls {
		if attr, ok := d.(*ast.Attribute); ok && !containsAttr(a, attr) {
			a = append(a, attr)
		}
	}
	return a
}

func containsAttr(a []*ast.Attribute, x *ast.Attribute) bool {
	for _, e := range a {
		if e.Text == x.Text {
			return true
		}
	}
	return false
}
