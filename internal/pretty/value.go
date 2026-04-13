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

package pretty

import (
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

// Node converts a CUE AST node into a Wadler-Lindig Doc.
func Node(n ast.Node) *Doc {
	switch n := n.(type) {
	case *ast.File:
		return fileDoc(n)
	case ast.Expr:
		return exprDoc(n)
	case ast.Decl:
		return declDoc(n)
	default:
		return Text("/* unknown node */")
	}
}

func fileDoc(f *ast.File) *Doc {
	var docs []*Doc
	for _, d := range f.Decls {
		docs = append(docs, declDoc(d))
	}
	if len(docs) == 0 {
		return Nil()
	}
	return Stack(docs...)
}

func declDoc(d ast.Decl) *Doc {
	switch d := d.(type) {
	case *ast.Package:
		return Spread(Text("package"), Text(d.Name.Name))

	case *ast.ImportDecl:
		return importDeclDoc(d)

	case *ast.Field:
		return fieldDoc(d)

	case *ast.Alias:
		return Spread(Text(d.Ident.Name), Text("="), exprDoc(d.Expr))

	case *ast.LetClause:
		return Spread(Text("let"), Text(d.Ident.Name), Text("="), exprDoc(d.Expr))

	case *ast.EmbedDecl:
		return exprDoc(d.Expr)

	case *ast.Ellipsis:
		if d.Type != nil {
			return Concat(Text("..."), exprDoc(d.Type))
		}
		return Text("...")

	case *ast.CommentGroup:
		return commentGroupDoc(d)

	case *ast.Comprehension:
		return comprehensionDoc(d)

	case *ast.Attribute:
		return Text(d.Text)

	case *ast.BadDecl:
		return Text("/* bad decl */")

	default:
		return Text("/* unknown decl */")
	}
}

// fieldDoc renders a field as a single document (label: value).
func fieldDoc(f *ast.Field) *Doc {
	label, value := fieldCells(f)
	return Concat(label, Concat(Text(" "), value))
}

// fieldCells returns the label and value parts of a field as separate
// documents, for use in table-based alignment. The label includes the
// constraint marker and colon (e.g. "name:", "port?:").
func fieldCells(f *ast.Field) (label, value *Doc) {
	lbl := labelDoc(f.Label)

	var constraint string
	switch f.Constraint {
	case token.OPTION:
		constraint = "?"
	case token.NOT:
		constraint = "!"
	}

	// Append postfix alias if present: label~X or label~(K,V)
	if f.Alias != nil {
		lbl = Concat(lbl, postfixAliasDoc(f.Alias))
	}

	// If the value is a struct with exactly one field (and no other decls
	// like embeddings, comprehensions, etc.), flatten into a: b: val form.
	// The chained labels all go into the label column.
	if s, ok := f.Value.(*ast.StructLit); ok && isSingleField(s) {
		innerLabel, innerValue := fieldCells(s.Elts[0].(*ast.Field))
		lbl = Concat(Concat(lbl, Text(constraint+":")), Concat(Text(" "), innerLabel))
		val := innerValue
		for _, a := range f.Attrs {
			val = Concat(val, Concat(Text(" "), Text(a.Text)))
		}
		return lbl, val
	}

	lbl = Concat(lbl, Text(constraint+":"))
	val := exprDoc(f.Value)

	// Append attributes to value.
	for _, a := range f.Attrs {
		val = Concat(val, Concat(Text(" "), Text(a.Text)))
	}
	return lbl, val
}

func postfixAliasDoc(a *ast.PostfixAlias) *Doc {
	if a.Label != nil {
		// Dual form: ~(K,V)
		return Concat(Text("~("),
			Concat(Text(a.Label.Name),
				Concat(Text(","),
					Concat(Text(a.Field.Name), Text(")")))))
	}
	// Simple form: ~X
	return Concat(Text("~"), Text(a.Field.Name))
}

// valueIsBlock reports whether a field's value is a struct or list literal.
func valueIsBlock(v ast.Expr) bool {
	switch v.(type) {
	case *ast.StructLit, *ast.ListLit:
		return true
	}
	return false
}

// isSingleField reports whether a struct literal contains exactly one element
// and that element is a regular field.
func isSingleField(s *ast.StructLit) bool {
	if len(s.Elts) != 1 {
		return false
	}
	_, ok := s.Elts[0].(*ast.Field)
	return ok
}

func labelDoc(l ast.Label) *Doc {
	switch l := l.(type) {
	case *ast.Ident:
		return Text(l.Name)
	case *ast.BasicLit:
		return Text(l.Value)
	case *ast.Interpolation:
		return interpolationDoc(l)
	case *ast.ListLit:
		// Pattern constraint label: [string]: ...
		return listDoc(l)
	case *ast.ParenExpr:
		return Concat(Text("("), Concat(exprDoc(l.X), Text(")")))
	default:
		return Text("/* unknown label */")
	}
}

func exprDoc(e ast.Expr) *Doc {
	if e == nil {
		return Nil()
	}
	switch e := e.(type) {
	case *ast.Ident:
		return Text(e.Name)

	case *ast.BasicLit:
		return Text(e.Value)

	case *ast.BottomLit:
		return Text("_|_")

	case *ast.StructLit:
		return structDoc(e)

	case *ast.ListLit:
		return listDoc(e)

	case *ast.Interpolation:
		return interpolationDoc(e)

	case *ast.BinaryExpr:
		return binaryDoc(e)

	case *ast.UnaryExpr:
		return Concat(Text(e.Op.String()), exprDoc(e.X))

	case *ast.PostfixExpr:
		return Concat(exprDoc(e.X), Text(e.Op.String()))

	case *ast.ParenExpr:
		return Concat(Text("("), Concat(exprDoc(e.X), Text(")")))

	case *ast.SelectorExpr:
		return Concat(exprDoc(e.X), Concat(Text("."), labelDoc(e.Sel)))

	case *ast.IndexExpr:
		return Concat(exprDoc(e.X),
			Concat(Text("["), Concat(exprDoc(e.Index), Text("]"))))

	case *ast.SliceExpr:
		return sliceDoc(e)

	case *ast.CallExpr:
		return callDoc(e)

	case *ast.Comprehension:
		return comprehensionDoc(e)

	case *ast.Ellipsis:
		if e.Type != nil {
			return Concat(Text("..."), exprDoc(e.Type))
		}
		return Text("...")

	case *ast.Func:
		return funcDoc(e)

	case *ast.BadExpr:
		return Text("/* bad expr */")

	default:
		return Text("/* unknown expr */")
	}
}

func structDoc(s *ast.StructLit) *Doc {
	if len(s.Elts) == 0 {
		return Text("{}")
	}

	var rows []TableRow
	for _, d := range s.Elts {
		if f, ok := d.(*ast.Field); ok {
			label, value := fieldCells(f)
			if valueIsBlock(f.Value) {
				rows = append(rows, NoAlignRow(label, value))
			} else {
				rows = append(rows, Row(label, value))
			}
		} else {
			// Non-field elements (embeddings, comprehensions, etc.)
			// occupy a single column spanning the full width.
			rows = append(rows, Row(declDoc(d)))
		}
	}

	table := Table(rows)
	return Bracket("{", 1, table, "}")
}

func listDoc(l *ast.ListLit) *Doc {
	if len(l.Elts) == 0 {
		return Text("[]")
	}

	var elems []*Doc
	for _, e := range l.Elts {
		elems = append(elems, exprDoc(e))
	}

	body := joinComma(elems)
	return Bracket("[", 1, body, "]")
}

func binaryDoc(b *ast.BinaryExpr) *Doc {
	left := exprDoc(b.X)
	right := exprDoc(b.Y)
	op := b.Op.String()
	// For & and | we want the group to allow line-breaking.
	return Group(Concat(left, Concat(Text(" "+op+" "), right)))
}

func interpolationDoc(interp *ast.Interpolation) *Doc {
	// Elts interleaves string fragments (BasicLit) and expressions.
	// The string fragments already contain the `\(` and `)` delimiters,
	// so we just emit each element directly.
	doc := Nil()
	for _, e := range interp.Elts {
		switch e := e.(type) {
		case *ast.BasicLit:
			doc = Concat(doc, Text(e.Value))
		default:
			doc = Concat(doc, exprDoc(e))
		}
	}
	return doc
}

func callDoc(c *ast.CallExpr) *Doc {
	fun := exprDoc(c.Fun)
	if len(c.Args) == 0 {
		return Concat(fun, Text("()"))
	}

	var args []*Doc
	for _, a := range c.Args {
		args = append(args, exprDoc(a))
	}

	body := joinComma(args)
	return Concat(fun, TightBracket("(", 1, body, ")"))
}

func sliceDoc(s *ast.SliceExpr) *Doc {
	low := Nil()
	if s.Low != nil {
		low = exprDoc(s.Low)
	}
	high := Nil()
	if s.High != nil {
		high = exprDoc(s.High)
	}
	return Concat(exprDoc(s.X),
		Concat(Text("["), Concat(low, Concat(Text(":"), Concat(high, Text("]"))))))
}

func comprehensionDoc(c *ast.Comprehension) *Doc {
	var clauses []*Doc
	for _, cl := range c.Clauses {
		clauses = append(clauses, clauseDoc(cl))
	}
	body := exprDoc(c.Value)
	doc := Group(Concat(Spread(clauses...), Concat(Text(" "), body)))
	if c.Fallback != nil {
		keyword := fallbackKeyword(c)
		doc = Concat(doc, Concat(Text(" "), Concat(Text(keyword),
			Concat(Text(" "), exprDoc(c.Fallback.Body)))))
	}
	return doc
}

// fallbackKeyword returns the appropriate keyword for a fallback clause.
// This mirrors the logic in cue/format/node.go: use "otherwise" for 'for'
// comprehensions or multi-clause comprehensions, "else" for 'if'/'try'.
func fallbackKeyword(c *ast.Comprehension) string {
	if len(c.Clauses) > 1 {
		return "otherwise"
	} else if _, ok := c.Clauses[0].(*ast.ForClause); ok {
		return "otherwise"
	}
	return "else"
}

func clauseDoc(c ast.Clause) *Doc {
	switch c := c.(type) {
	case *ast.ForClause:
		parts := []*Doc{Text("for")}
		if c.Key != nil {
			parts = append(parts, Text(c.Key.Name+","))
		}
		parts = append(parts, Text(c.Value.Name), Text("in"), exprDoc(c.Source))
		return Spread(parts...)

	case *ast.IfClause:
		return Spread(Text("if"), exprDoc(c.Condition))

	case *ast.LetClause:
		return Spread(Text("let"), Text(c.Ident.Name), Text("="), exprDoc(c.Expr))

	case *ast.TryClause:
		if c.Ident != nil {
			// Assignment form: try x = expr
			return Spread(Text("try"), Text(c.Ident.Name), Text("="), exprDoc(c.Expr))
		}
		// Struct form: try (body is in Comprehension.Value)
		return Text("try")

	default:
		return Text("/* unknown clause */")
	}
}

func funcDoc(f *ast.Func) *Doc {
	var args []*Doc
	for _, a := range f.Args {
		args = append(args, exprDoc(a))
	}
	params := joinComma(args)
	return Concat(Text("func"), Concat(Bracket("(", 1, params, ")"), Concat(Text(" "), exprDoc(f.Ret))))
}

func importDeclDoc(d *ast.ImportDecl) *Doc {
	if len(d.Specs) == 1 {
		return Spread(Text("import"), importSpecDoc(d.Specs[0]))
	}
	var specs []*Doc
	for _, s := range d.Specs {
		specs = append(specs, importSpecDoc(s))
	}
	body := Stack(specs...)
	return Concat(Text("import"), Concat(Text(" "), Bracket("(", 1, body, ")")))
}

func importSpecDoc(s *ast.ImportSpec) *Doc {
	if s.Name != nil {
		return Spread(Text(s.Name.Name), Text(s.Path.Value))
	}
	return Text(s.Path.Value)
}

func commentGroupDoc(g *ast.CommentGroup) *Doc {
	var docs []*Doc
	for _, c := range g.List {
		docs = append(docs, Text(c.Text))
	}
	return Stack(docs...)
}

// joinComma joins documents with ", " in flat mode or ",\n" in broken mode.
func joinComma(docs []*Doc) *Doc {
	if len(docs) == 0 {
		return Nil()
	}
	result := docs[0]
	for _, d := range docs[1:] {
		result = Concat(Concat(result, Text(",")), Concat(Line(), d))
	}
	return result
}
