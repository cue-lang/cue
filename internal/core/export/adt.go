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
	"bytes"
	"cmp"
	"fmt"
	"slices"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/adt"
)

func (e *exporter) ident(x adt.Feature) *ast.Ident {
	s := e.identString(x)
	if !ast.IsValidIdent(s) {
		panic(s + " is not a valid identifier")
	}
	return ast.NewIdent(s)
}

func (e *exporter) adt(env *adt.Environment, expr adt.Elem) ast.Expr {
	switch x := expr.(type) {
	case adt.Value:
		return e.expr(env, x)

	case *adt.ListLit:
		env := &adt.Environment{Up: env, Vertex: e.node()}
		a := []ast.Expr{}
		for _, x := range x.Elems {
			a = append(a, e.elem(env, x))
		}
		return ast.NewList(a...)

	case *adt.StructLit:
		// TODO: should we use pushFrame here?
		// _, saved := e.pushFrame([]adt.Conjunct{adt.MakeConjunct(nil, x)})
		// defer e.popFrame(saved)
		// s := e.frame(0, false).scope
		s := &ast.StructLit{}
		env := e.newEnv(env)

		for _, d := range x.Decls {
			var aliasName string
			if orig, ok := d.Source().(*ast.Field); ok {
				if alias, ok := orig.Value.(*ast.Alias); ok {
					if e.valueAlias == nil {
						e.valueAlias = map[*ast.Alias]string{}
					}
					aliasName = alias.Ident.Name
					e.valueAlias[alias] = aliasName
				}
			}
			decl := e.decl(env, d)

			// decl is nil for a let not marked in this scope. A marked one
			// is removed later if it is never used.
			if decl == nil {
				continue
			}

			if e.cfg.ShowDocs {
				ast.SetComments(decl, leadingDocComments(ast.Comments(d.Source())))
			}
			// TODO: use e.copyMeta for positions, but only when the original
			// source is available.

			if aliasName != "" {
				if f, ok := decl.(*ast.Field); ok {
					f.Value = e.bindValueAlias(aliasName, f.Value)
				}
			}

			s.Elts = append(s.Elts, decl)
		}

		return s

	// TODO: why does LabelReference not implement resolve?
	case *adt.LabelReference:
		// get potential label from Source. Otherwise use X.
		v, ok := e.ctx.Evaluate(env, x)
		// needField skips intermediate mergeValues frames from nested
		// fields that don't carry the BulkOptionalField's field info.
		f := e.frame(x.UpCount, true)
		if ok && (adt.IsConcrete(v) || f.field == nil) {
			return e.value(v)
		}
		if f.field == nil {
			// This can happen when the LabelReference is evaluated outside of
			// normal evaluation, that is, if a pattern constraint or
			// additional constraint is evaluated by itself.
			return ast.NewIdent("string")
		}
		name, ok := labelAliasName(f.field)
		if !ok {
			if x.Src != nil {
				name = x.Src.Name
			}
			name = e.uniqueAlias(name)
			e.setLabelAlias(f.field, name)
		}
		ident := ast.NewIdent(name)
		ident.Scope = f.field
		ident.Node = f.labelExpr
		return ident

	case adt.Resolver:
		return e.resolve(env, x)

	case *adt.SliceExpr:
		var lo, hi ast.Expr
		if x.Lo != nil {
			lo = e.innerExpr(env, x.Lo)
		}
		if x.Hi != nil {
			hi = e.innerExpr(env, x.Hi)
		}
		// TODO: Stride not yet? implemented.
		// if x.Stride != nil {
		// 	stride = e.innerExpr(env, x.Stride)
		// }
		return &ast.SliceExpr{X: e.innerExpr(env, x.X), Low: lo, High: hi}

	case *adt.Interpolation:
		var (
			tripple    = `"""`
			openQuote  = `"`
			closeQuote = `"`
			f          = literal.String
		)
		if x.K&adt.BytesKind != 0 {
			tripple = `'''`
			openQuote = `'`
			closeQuote = `'`
			f = literal.Bytes
		}
		toString := func(v adt.Expr) string {
			str := ""
			switch x := v.(type) {
			case *adt.String:
				str = x.Str
			case *adt.Bytes:
				str = string(x.B)
			}
			return str
		}
		t := &ast.Interpolation{}
		f = f.WithGraphicOnly()
		indent := ""
		// TODO: mark formatting in interpolation itself.
		for i := 0; i < len(x.Parts); i += 2 {
			if strings.IndexByte(toString(x.Parts[i]), '\n') >= 0 {
				f = f.WithTabIndent(len(e.stack))
				indent = strings.Repeat("\t", len(e.stack))
				openQuote = tripple + "\n" + indent
				closeQuote = tripple
				break
			}
		}
		prefix := openQuote
		suffix := `\(`
		for i, elem := range x.Parts {
			if i%2 == 1 {
				t.Elts = append(t.Elts, e.innerExpr(env, elem))
			} else {
				// b := strings.Builder{}
				buf := []byte(prefix)
				str := toString(elem)
				buf = f.AppendEscaped(buf, str)
				if i == len(x.Parts)-1 {
					if len(closeQuote) > 1 {
						buf = append(buf, '\n')
						buf = append(buf, indent...)
					}
					buf = append(buf, closeQuote...)
				} else {
					if bytes.HasSuffix(buf, []byte("\n")) {
						buf = append(buf, indent...)
					}
					buf = append(buf, suffix...)
				}
				t.Elts = append(t.Elts, &ast.BasicLit{
					Kind:  token.STRING,
					Value: string(buf),
				})
			}
			prefix = ")"
		}
		return t

	case *adt.BoundExpr:
		return &ast.UnaryExpr{
			Op: x.Op.Token(),
			X:  e.innerExpr(env, x.Expr),
		}

	case *adt.UnaryExpr:
		return &ast.UnaryExpr{
			Op: x.Op.Token(),
			X:  e.innerExpr(env, x.X),
		}

	case *adt.OpenExpr:
		return &ast.PostfixExpr{
			X:  e.innerExpr(env, x.X),
			Op: token.ELLIPSIS,
		}

	case *adt.Function:
		return e.funcSrc(x.Src)

	case *adt.BinaryExpr:
		if x.Op == adt.AndOp || x.Op == adt.OrOp {
			return e.sortBinaryTree(env, x)
		}
		return &ast.BinaryExpr{
			Op: x.Op.Token(),
			X:  e.innerExpr(env, x.X),
			Y:  e.innerExpr(env, x.Y),
		}

	case *adt.CallExpr:
		a := []ast.Expr{}
		labels := []ast.Label(nil)
		for _, arg := range x.Args {
			v := e.innerExpr(env, arg)
			if v == nil {
				panic(fmt.Sprintf("cannot export call argument %T", arg))
			}
			a = append(a, v)
		}
		if x.ArgLabels != nil {
			labels = make([]ast.Label, len(x.ArgLabels))
			for i, label := range x.ArgLabels {
				if label != adt.InvalidLabel {
					labels[i] = ast.NewIdent(label.IdentString(e.ctx))
				}
			}
		}
		fun := e.innerExpr(env, x.Fun)
		call := &ast.CallExpr{Fun: fun, Args: a, ArgLabels: labels}
		if x.Partial {
			// The position is synthetic, but must be set for the
			// formatter to render the "..." marking the partial
			// application.
			call.Ellipsis = token.Blank.Pos()
		}
		return call

	case *adt.DisjunctionExpr:
		a := []ast.Expr{}
		for _, d := range x.Values {
			v := e.expr(env, d.Val)
			if d.Default {
				v = &ast.UnaryExpr{Op: token.MUL, X: v}
			}
			a = append(a, v)
		}
		return ast.NewBinExpr(token.OR, a...)

	case *adt.ConjunctGroup:
		a := []ast.Expr{}
		for _, c := range *x {
			v := e.expr(c.EnvExpr())
			a = append(a, v)
		}
		return ast.NewBinExpr(token.AND, a...)

	default:
		panic(fmt.Sprintf("unknown field %T", x))
	}
}

// sortBinaryTree converte x to a binary tree and sorts it's elements
// using sortLeafAdt.
func (e *exporter) sortBinaryTree(env *adt.Environment, x *adt.BinaryExpr) (b ast.Expr) {
	var exprs []adt.Node

	var flatten func(expr adt.Expr)
	flatten = func(expr adt.Expr) {
		if y, ok := expr.(*adt.BinaryExpr); ok && x.Op == y.Op {
			flatten(y.X)
			flatten(y.Y)
		} else {
			exprs = append(exprs, expr)
		}
	}
	flatten(x)

	// Sort the expressions
	slices.SortStableFunc(exprs, cmpLeafNodes)

	nodes := make([]ast.Expr, 0, len(exprs))
	for _, x := range exprs {
		switch y := x.(type) {
		case *adt.Top:
		case *adt.BasicType:
			if y.K != adt.TopKind {
				nodes = append(nodes, e.expr(env, y))
			}
		default:
			nodes = append(nodes, e.innerExpr(env, y.(adt.Expr)))
		}
	}

	if len(nodes) == 0 {
		return e.adt(env, &adt.Top{})
	}

	return ast.NewBinExpr(x.Op.Token(), nodes...)
}

// cmpConjuncts compares two Conjunct based on their element using cmpLeafNodes.
func cmpConjuncts(a, b adt.Conjunct) int {
	return cmpLeafNodes(a.Expr(), b.Expr())
}

// cmpLeafNodes compares two adt.Expr values. The values may not be a binary
// expressions. It returns true if a is less than b.
func cmpLeafNodes[T adt.Node](a, b T) int {
	if c := cmp.Compare(typeOrder(a), typeOrder(b)); c != 0 {
		return c
	}

	srcA := a.Source()
	srcB := b.Source()

	if srcA == nil || srcB == nil {
		// TODO: some tie breaker
		return 0
	}

	return srcA.Pos().Compare(srcB.Pos())
}

func typeOrder(x adt.Node) int {
	switch x.(type) {
	case *adt.Top:
		return 0
	case *adt.BasicType:
		return 1
	case *adt.FieldReference:
		return 2 // sometimes basic types are represented as field references.
	case *adt.Bool, *adt.Null, *adt.Num, *adt.String, *adt.Bytes:
		return 10
	case *adt.BoundValue:
		return 20
	case *adt.StructLit, *adt.ListLit:
		return 500
	case adt.Expr:
		return 25
	default:
		return 100
	}
}

var dummyTop = &ast.Ident{Name: "_"}

func wrapIfOptional(expr ast.Expr, isOptional bool) ast.Expr {
	if isOptional {
		return &ast.PostfixExpr{X: expr, Op: token.OPTION}
	}
	return expr
}

func (e *exporter) resolve(env *adt.Environment, r adt.Resolver) ast.Expr {
	if c := e.pivotter; c != nil {
		if alt := c.refExpr(r); alt != nil {
			return alt
		}
	}

	switch x := r.(type) {
	case *adt.FieldReference:
		// Special case when the original CUE already had an alias.
		if x.Src != nil {
			if f, ok := x.Src.Node.(*ast.Field); ok {
				if field, scope := e.lookupFieldAlias(f); field != nil {
					name, _ := valueAliasName(field)
					ident := ast.NewIdent(name)
					ident.Node = field
					ident.Scope = scope
					return wrapIfOptional(ident, x.Optional)
				}
				// The field is not exported with its alias, such as when
				// exporting the arcs of a value. Its label may be shadowed
				// at the reference, so keep the alias, which fails to
				// resolve rather than silently binding to another field.
				if name, ok := valueAliasName(f); ok {
					return wrapIfOptional(ast.NewIdent(name), x.Optional)
				}
			}
		}

		ident := e.newIdentForField(x.Src, x.Label, e.fieldsOf(env, x.UpCount))
		return wrapIfOptional(ident, x.Optional)

	case *adt.ValueReference:
		name := x.Label.IdentString(e.ctx)
		if a, ok := x.Src.Node.(*ast.Alias); ok { // Should always pass
			if b, ok := e.valueAlias[a]; ok {
				name = b
			}
		}
		ident := ast.NewIdent(name)
		return ident

	case *adt.DynamicReference:
		name := "X"
		if x.Src != nil {
			name = x.Src.Name
		}
		// The innermost dynamic field with the alias is the one referenced.
		var f *ast.Field
	stack:
		for i := len(e.stack) - 1; i >= 0; i-- {
			for _, entry := range e.stack[i].dynamicFields {
				if entry.alias == name {
					f = entry.field
					break stack
				}
			}
		}

		if f != nil {
			name = e.getFieldAlias(f, name)
		}

		ident := ast.NewIdent(name)
		ident.Scope = f
		ident.Node = f
		return ident

	case *adt.ImportReference:
		importPath := x.ImportPath.StringValue(e.index)
		info := ast.ParseImportPath(importPath)
		spec := ast.NewImport(nil, importPath)

		name := info.Qualifier
		if x.Label != 0 {
			name = x.Label.IdentString(e.index)
			if name != info.Qualifier {
				spec.Name = ast.NewIdent(name)
			}
		}
		ident := ast.NewIdent(name)
		ident.Node = spec
		return ident

	case *adt.LetReference:
		return e.resolveLet(env, x)

	case *adt.FuncCallRef:
		// A call reference is a pure resolver to the called function's
		// anchor vertex; the call it schedules is described by its source,
		// the original call expression. Note that the generic adt.Resolver
		// case dispatches here before the type switch in adt reaches any
		// more specific case. The source is cloned, as finalize edits the
		// output in place.
		if src, ok := x.Source().(ast.Expr); ok {
			return ast.Clone(src)
		}
		return ast.NewIdent("_")

	case *adt.SelectorExpr:
		sel := &ast.SelectorExpr{
			X:   e.innerExpr(env, x.X),
			Sel: e.stringLabel(x.Sel),
		}
		return wrapIfOptional(sel, x.Optional)

	case *adt.IndexExpr:
		idx := &ast.IndexExpr{
			X:     e.innerExpr(env, x.X),
			Index: e.innerExpr(env, x.Index),
		}
		return wrapIfOptional(idx, x.Optional)
	}
	panic("unreachable")
}

// newIdentForField returns an identifier referring to label among fields,
// the fields of the frame declaring it, or nil for no frame. The identifier
// is linked to the declaration when that frame is popped.
func (e *exporter) newIdentForField(
	orig *ast.Ident,
	label adt.Feature,
	fields map[adt.Feature]entry) *ast.Ident {
	entry := fields[label]

	name := e.identString(label)
	switch {
	case entry.alias != "":
		name = entry.alias

	case !ast.IsValidIdent(name):
		name = "X"
		if orig != nil {
			name = orig.Name
		}
		name = e.uniqueAlias(name)
		entry.alias = name
	}

	ident := ast.NewIdent(name)
	entry.references = append(entry.references, ident)

	if fields != nil {
		fields[label] = entry
	}

	return ident
}

func (e *exporter) decl(env *adt.Environment, d adt.Decl) ast.Decl {
	switch x := d.(type) {
	case adt.Elem:
		return e.elem(env, x)

	case *adt.Field:
		e.setDocs(x)
		f := e.getFixedField(x)
		f.Constraint = x.ArcType.Token()
		e.addField(x.Label, f)

		f.Attrs = e.exportAttrs(extractFieldAttrs(nil, x))

		if st, ok := x.Value.(*adt.StructLit); ok {
			// Instead of calling e.expr directly, we inline the case for
			// *adt.StructLit, so that we can pass src.
			var src *adt.Vertex
			if top := e.top(); top.node != nil {
				src = top.node.Lookup(x.Label)
			}
			c := adt.MakeRootConjunct(env, st)
			f.Value = e.mergeValues(adt.InvalidLabel, src, []conjunct{{c: c, up: 0}}, c)
		} else {
			f.Value = e.expr(env, x.Value)
		}
		return f

	case *adt.LetField:
		// Declared where its source is if marked in this scope.
		if let, ok := e.top().letAlias[x.Src]; ok {
			return let
		}
		return nil

	case *adt.BulkOptionalField:
		e.setDocs(x)
		// set bulk in frame.
		frame := e.frame(0, false)

		expr := e.innerExpr(env, x.Filter)
		frame.labelExpr = expr // see astutil.Resolve.

		f := &ast.Field{
			Label: ast.NewList(expr),
		}
		if x.Label != 0 {
			// Mark features used in the value expression so that
			// uniqueAlias can avoid generating a conflicting name
			// when the alias has the same name as a field in the value.
			e.markUsedFeatures(x.Value)
			e.setLabelAlias(f, e.uniqueAlias(e.identString(x.Label)))
		}

		frame.field = f

		if alias, ok := valueAliasName(x.Src); ok {
			frame.dynamicFields = append(frame.dynamicFields, &entry{
				alias: alias,
				field: f,
			})
		}

		f.Value = e.expr(env, x.Value)
		f.Attrs = e.exportAttrs(extractFieldAttrs(nil, x))

		return f

	case *adt.DynamicField:
		e.setDocs(x)
		srcKey := x.Key

		f := &ast.Field{Constraint: x.ArcType.Token()}

		v, _ := e.ctx.Evaluate(env, x.Key)

		switch s, ok := v.(*adt.String); {
		// TODO(unshadow): allow once unshadowing algorithm is fixed.
		// case ok && ast.IsValidIdent(s.Str):
		// 	label := e.ctx.StringLabel(s.Str)
		// 	f.Label = ast.NewIdent(s.Str)
		// 	e.addField(label, f)

		case ok:
			srcKey = s

			fallthrough

		default:
			key := e.innerExpr(env, srcKey)
			switch key.(type) {
			case *ast.Interpolation, *ast.BasicLit:
			default:
				key = &ast.ParenExpr{X: key}
			}
			f.Label = key.(ast.Label)
		}

		alias, _ := valueAliasName(x.Src)

		frame := e.frame(0, false)
		frame.dynamicFields = append(frame.dynamicFields, &entry{
			alias: alias,
			field: f,
		})

		f.Value = e.expr(env, x.Value)
		f.Attrs = e.exportAttrs(extractFieldAttrs(nil, x))

		return f

	default:
		panic(fmt.Sprintf("unknown field %T", x))
	}
}

func (e *exporter) copyMeta(dst, src ast.Node) {
	if e.cfg.ShowDocs {
		ast.SetComments(dst, leadingDocComments(ast.Comments(src)))
	}
	astutil.CopyPosition(dst, src)
}

func (e *exporter) elem(env *adt.Environment, d adt.Elem) ast.Expr {

	switch x := d.(type) {
	case adt.Expr:
		return e.expr(env, x)

	case *adt.Ellipsis:
		t := &ast.Ellipsis{}
		if x.Value != nil {
			t.Type = e.expr(env, x.Value)
		}
		return t

	case *adt.Comprehension:
		return e.comprehension(env, x)

	default:
		panic(fmt.Sprintf("unknown field %T", x))
	}
}

// clauseVar declares the clause variable label in the top frame, and returns
// the identifier its references are linked to.
func (e *exporter) clauseVar(label adt.Feature) *ast.Ident {
	id := e.ident(label)
	e.top().fields[label] = entry{ident: id}
	return id
}

func (e *exporter) comprehension(env *adt.Environment, comp *adt.Comprehension) *ast.Comprehension {
	c := &ast.Comprehension{}

	// Save outer environment for else clause (which doesn't have access to
	// comprehension-internal bindings).
	outerEnv := env

	// A clause's expression is in the enclosing scope, and its variable
	// in a new scope for the clauses after it and the body.
	for _, y := range comp.Clauses {
		switch x := y.(type) {
		case *adt.ForClause:
			src := e.innerExpr(env, x.Src)
			_, saved := e.pushFrame(empty, nil)
			defer e.popFrame(saved)
			env = e.newEnv(env)
			clause := &ast.ForClause{Value: e.clauseVar(x.Value), Source: src}
			if x.Key != adt.InvalidLabel {
				clause.Key = e.clauseVar(x.Key)
			} else if x.Syntax != nil && x.Syntax.Key != nil {
				// A blank key declares no variable.
				clause.Key = e.ident(x.Key)
			}
			e.copyMeta(clause, x.Syntax)
			c.Clauses = append(c.Clauses, clause)

		case *adt.IfClause:
			cond := e.innerExpr(env, x.Condition)
			clause := &ast.IfClause{Condition: cond}
			e.copyMeta(clause, x.Src)
			c.Clauses = append(c.Clauses, clause)

		case *adt.LetClause:
			expr := e.innerExpr(env, x.Expr)
			_, saved := e.pushFrame(empty, nil)
			defer e.popFrame(saved)
			env = e.newEnv(env)
			clause := &ast.LetClause{
				Ident: e.clauseVar(x.Label),
				Expr:  expr,
			}
			e.copyMeta(clause, x.Src)
			c.Clauses = append(c.Clauses, clause)

		case *adt.TryClause:
			clause := &ast.TryClause{}
			if x.Label != adt.InvalidLabel {
				// Assignment form: try x = expr
				clause.Expr = e.innerExpr(env, x.Expr)
				_, saved := e.pushFrame(empty, nil)
				defer e.popFrame(saved)
				env = e.newEnv(env)
				clause.Ident = e.clauseVar(x.Label)
			}
			// Struct form has no Ident/Expr - body is in Comprehension.Value
			e.copyMeta(clause, x.Src)
			c.Clauses = append(c.Clauses, clause)

		default:
			panic(fmt.Sprintf("unknown field %T", x))
		}
	}
	e.copyMeta(c, comp.Syntax)

	// TODO: consider using adt.EnvExpr.
	v := e.expr(env, adt.ToExpr(comp.Value))
	if _, ok := v.(*ast.StructLit); !ok {
		v = ast.NewStruct(ast.Embed(v))
	}
	c.Value = v

	// Export fallback clause using outer environment.
	if comp.Fallback != nil {
		fallbackBody := e.expr(outerEnv, comp.Fallback)
		if body, ok := fallbackBody.(*ast.StructLit); ok {
			c.Fallback = &ast.FallbackClause{Body: body}
		}
	}

	return c
}
