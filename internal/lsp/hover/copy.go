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

package hover

import (
	"reflect"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/unstable/lsp/eval"
)

// copier deep-copies AST subtrees for the [renderer], replacing every
// reference it encounters with the rendering of what the reference
// refers to (see [renderer.inlineReference]). The copies share no
// nodes with the original: they carry no position information (every
// [token.Pos] field is the zero value, [token.NoPos]), no comments
// other than doc comments, and no resolution back-references
// ([ast.Ident.Scope] and [ast.Ident.Node]). This makes them safe to
// graft into a synthetic tree: the pretty printer treats a
// position-free tree as programmatic and lays it out with its
// width-driven heuristics.
type copier struct {
	r *renderer
	// d is the declaration whose syntax is being copied; references
	// found within it are resolved via [eval.Decl.Resolve].
	d *eval.Decl
}

var (
	posType       = reflect.TypeFor[token.Pos]()
	identType     = reflect.TypeFor[ast.Ident]()
	exprType      = reflect.TypeFor[ast.Expr]()
	callExprType  = reflect.TypeFor[ast.CallExpr]()
	structLitType = reflect.TypeFor[ast.StructLit]()
	listLitType   = reflect.TypeFor[ast.ListLit]()

	// parenOwners are the nodes within which an inlined binary
	// expression would regroup with its surroundings, and so must be
	// parenthesized. In all other positions (call arguments, list
	// elements, field values, ...) the replacement is self-delimited.
	parenOwners = map[reflect.Type]bool{
		reflect.TypeFor[ast.BinaryExpr]():   true,
		reflect.TypeFor[ast.UnaryExpr]():    true,
		reflect.TypeFor[ast.SelectorExpr](): true,
		reflect.TypeFor[ast.IndexExpr]():    true,
		reflect.TypeFor[ast.SliceExpr]():    true,
	}
)

// node returns a deep copy of the AST rooted at n, with references
// inlined.
func (c copier) node(n ast.Node) ast.Node {
	if n == nil {
		return nil
	}
	return c.value(nil, reflect.ValueOf(n)).Interface().(ast.Node)
}

// value structurally copies v: pointers, interfaces and slices are
// copied recursively, and structs are copied field-wise. Unexported
// fields (comments, and the marker types that make nodes implement
// the ast interfaces) are left at their zero values, as are
// [token.Pos] fields and the back-reference fields of [ast.Ident].
// Everything else (strings, bools, tokens) is copied by value.
//
// owner is the struct type whose field or slice slot v occupies (nil
// at the root): reference inlining applies only to slots whose static
// type is [ast.Expr], and how a replacement must be parenthesized
// depends on the owner.
func (c copier) value(owner reflect.Type, v reflect.Value) reflect.Value {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return v
		}
		out := reflect.New(v.Type().Elem())
		out.Elem().Set(c.value(owner, v.Elem()))
		// Comments live in unexported storage, which the field-wise
		// struct copy leaves empty; doc comments are worth keeping.
		if orig, ok := v.Interface().(ast.Node); ok {
			if docs := c.docComments(orig); docs != nil {
				ast.SetComments(out.Interface().(ast.Node), docs)
			}
		}
		return out

	case reflect.Interface:
		if v.IsNil() {
			return v
		}
		// The copied concrete value is assignable wherever the
		// interface value came from.
		return c.value(owner, v.Elem())

	case reflect.Slice:
		if v.IsNil() {
			return v
		}
		out := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		elemType := v.Type().Elem()
		for i := range v.Len() {
			out.Index(i).Set(c.slot(owner, "", elemType, v.Index(i)))
		}
		return out

	case reflect.Struct:
		c.r.countNode()
		t := v.Type()
		if t == structLitType || t == listLitType {
			// The copy's contents nest one level deeper in the
			// output; see [maxInlineDepth].
			c.r.depth++
			defer func() { c.r.depth-- }()
		}
		out := reflect.New(t).Elem()
		for i := range t.NumField() {
			f := t.Field(i)
			if !f.IsExported() || f.Type == posType {
				continue
			}
			if t == identType && (f.Name == "Scope" || f.Name == "Node") {
				// An ident's resolution back-references point into
				// foreign ASTs; the copy must not retain them.
				continue
			}
			out.Field(i).Set(c.slot(t, f.Name, f.Type, v.Field(i)))
		}
		return out

	default:
		return v
	}
}

// slot copies the value v occupying a slot — the owner struct type's
// named field, or an element of a slice field ("") — with the given
// static type. A slot of static type [ast.Expr] holds a value in
// expression position, where a reference can be replaced by what it
// refers to; slots of any other static type (labels, binding idents,
// a selector's Sel) hold names or declarations, which are copied as
// written. The one expression slot that must not be inlined is a
// call's callee: `f` names the function in `f(x)`, and replacing it
// with the function value would be nonsense.
func (c copier) slot(owner reflect.Type, field string, static reflect.Type, v reflect.Value) reflect.Value {
	if static != exprType || v.IsNil() {
		return c.value(owner, v)
	}
	if owner == callExprType && field == "Fun" {
		return c.value(owner, v)
	}
	replacement, ok := c.r.inlineReference(c.d, v.Interface().(ast.Expr))
	if !ok {
		return c.value(owner, v)
	}
	if _, isBin := replacement.(*ast.BinaryExpr); isBin && parenOwners[owner] {
		c.r.countNode()
		replacement = &ast.ParenExpr{X: replacement}
	}
	return reflect.ValueOf(replacement)
}

// docComments returns position-free copies of the doc comment groups
// that semantically document n, or nil if there are none. Note
// [ast.DocComments] resolves the field-chain convention, so, given:
//
//	// comment
//	y: z: 3
//
// the comment — syntactically attached to the Field y — is reported
// for (and so copied onto) the chain's leaf Field z. Rendering y's
// value as `z: 3` then keeps the comment with the field it
// documents, while a comment documenting a label that the rendering
// omits is dropped along with the label.
func (c copier) docComments(n ast.Node) []*ast.CommentGroup {
	var docs []*ast.CommentGroup
	for _, cg := range ast.DocComments(n) {
		cg := c.value(nil, reflect.ValueOf(cg)).Interface().(*ast.CommentGroup)
		// The comment may have been inherited from a field-chain
		// head, where it was not necessarily a Doc group of the leaf
		// itself; on the copy it must be one for the printer to place
		// it above the node.
		cg.Doc = true
		docs = append(docs, cg)
	}
	return docs
}
