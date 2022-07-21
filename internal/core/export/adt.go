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
	"fmt"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/adt"
)

func (e *exporter) ident(x adt.Feature) *ast.Ident {
	s := x.IdentString(e.ctx)
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
		// s := e.frame(0).scope

		s := &ast.StructLit{}
		env := &adt.Environment{Up: env, Vertex: e.node()}

		for _, d := range x.Decls {
			var a *ast.Alias
			if orig, ok := d.Source().(*ast.Field); ok {
				if alias, ok := orig.Value.(*ast.Alias); ok {
					if e.valueAlias == nil {
						e.valueAlias = map[*ast.Alias]*ast.Alias{}
					}
					a = &ast.Alias{Ident: ast.NewIdent(alias.Ident.Name)}
					e.valueAlias[alias] = a
				}
			}
			decl := e.decl(env, d)

			if a != nil {
				if f, ok := decl.(*ast.Field); ok {
					a.Expr = f.Value
					f.Value = a
				}
			}

			s.Elts = append(s.Elts, decl)
		}

		return s

	// TODO: why does LabelReference not implement resolve?
	case *adt.LabelReference:
		// get potential label from Source. Otherwise use X.
		f := e.frame(x.UpCount)
		if f.field == nil {
			// This can happen when the LabelReference is evaluated outside of
			// normal evaluation, that is, if a pattern constraint or
			// additional constraint is evaluated by itself.
			return ast.NewIdent("string")
		}
		list, ok := f.field.Label.(*ast.ListLit)
		if !ok || len(list.Elts) != 1 {
			panic("label reference to non-pattern constraint field or invalid list")
		}
		name := ""
		if a, ok := list.Elts[0].(*ast.Alias); ok {
			name = a.Ident.Name
		} else {
			if x.Src != nil {
				name = x.Src.Name
			}
			name = e.uniqueAlias(name)
			list.Elts[0] = &ast.Alias{
				Ident: ast.NewIdent(name),
				Expr:  list.Elts[0],
			}
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
			lo = e.expr(env, x.Lo)
		}
		if x.Hi != nil {
			hi = e.expr(env, x.Hi)
		}
		// TODO: Stride not yet? implemented.
		// if x.Stride != nil {
		// 	stride = e.expr(env, x.Stride)
		// }
		return &ast.SliceExpr{X: e.expr(env, x.X), Low: lo, High: hi}

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
				t.Elts = append(t.Elts, e.expr(env, elem))
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
			X:  e.expr(env, x.Expr),
		}

	case *adt.UnaryExpr:
		return &ast.UnaryExpr{
			Op: x.Op.Token(),
			X:  e.expr(env, x.X),
		}

	case *adt.BinaryExpr:
		return &ast.BinaryExpr{
			Op: x.Op.Token(),
			X:  e.expr(env, x.X),
			Y:  e.expr(env, x.Y),
		}

	case *adt.CallExpr:
		a := []ast.Expr{}
		for _, arg := range x.Args {
			v := e.expr(env, arg)
			if v == nil {
				e.expr(env, arg)
				panic("")
			}
			a = append(a, v)
		}
		fun := e.expr(env, x.Fun)
		return &ast.CallExpr{Fun: fun, Args: a}

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

	case *adt.Comprehension:
		if !x.DidResolve() {
			return dummyTop
		}
	loop:
		for c := x.Clauses; ; {
			switch x := c.(type) {
			case *adt.ForClause:
				env = &adt.Environment{Up: env, Vertex: empty}
				c = x.Dst
			case *adt.IfClause:
				c = x.Dst
			case *adt.LetClause:
				env = &adt.Environment{Up: env, Vertex: empty}
				c = x.Dst
			case *adt.ValueClause:
				break loop
			default:
				panic("unreachable")
			}
		}
		return e.adt(env, adt.ToExpr(x.Value))

	default:
		panic(fmt.Sprintf("unknown field %T", x))
	}
}

var dummyTop = &ast.Ident{Name: "_"}

func (e *exporter) resolve(env *adt.Environment, r adt.Resolver) ast.Expr {
	switch x := r.(type) {
	case *adt.FieldReference:
		ident, _ := e.newIdentForField(x.Src, x.Label, x.UpCount)
		return ident

	case *adt.ValueReference:
		name := x.Label.IdentString(e.ctx)
		if a, ok := x.Src.Node.(*ast.Alias); ok { // Should always pass
			if b, ok := e.valueAlias[a]; ok {
				name = b.Ident.Name
			}
		}
		ident := ast.NewIdent(name)
		return ident

	case *adt.DynamicReference:
		// TODO(dynamic): ensure we correctly unshadow newly visible fields.
		//   before uncommenting this.
		//
		// if v := x.EvaluateLabel(e.ctx, env); v != 0 {
		// 	str := v.StringValue(e.ctx)
		// 	if ast.IsValidIdent(str) {
		// 		return ast.NewIdent(str)
		// 	}
		// }

		name := "X"
		if x.Src != nil {
			name = x.Src.Name
		}
		var f *ast.Field
		for i := len(e.stack) - 1; i >= 0; i-- {
			for _, entry := range e.stack[i].dynamicFields {
				if entry.alias == name {
					f = entry.field
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
		spec := ast.NewImport(nil, importPath)

		info, _ := astutil.ParseImportSpec(spec)
		name := info.PkgName
		if x.Label != 0 {
			name = x.Label.StringValue(e.index)
			if name != info.PkgName {
				spec.Name = ast.NewIdent(name)
			}
		}
		ident := ast.NewIdent(name)
		ident.Node = spec
		return ident

	case *adt.LetReference:
		return e.resolveLet(env, x)

	case *adt.SelectorExpr:
		return &ast.SelectorExpr{
			X:   e.expr(env, x.X),
			Sel: e.stringLabel(x.Sel),
		}

	case *adt.IndexExpr:
		return &ast.IndexExpr{
			X:     e.expr(env, x.X),
			Index: e.expr(env, x.Index),
		}
	}
	panic("unreachable")
}

func (e *exporter) newIdentForField(
	orig *ast.Ident,
	label adt.Feature,
	upCount int32) (ident *ast.Ident, ok bool) {
	f := e.frame(upCount)
	entry := f.fields[label]

	name := label.IdentString(e.ctx)
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

	ident = ast.NewIdent(name)
	entry.references = append(entry.references, ident)

	if f.fields != nil {
		f.fields[label] = entry
		ok = true
	}

	return ident, ok
}

func (e *exporter) decl(env *adt.Environment, d adt.Decl) ast.Decl {
	switch x := d.(type) {
	case adt.Elem:
		return e.elem(env, x)

	case *adt.Field:
		e.setDocs(x)
		f := &ast.Field{
			Label: e.stringLabel(x.Label),
		}

		e.setField(x.Label, f)

		f.Value = e.expr(env, x.Value)

		// extractDocs(nil)
		return f

	case *adt.OptionalField:
		e.setDocs(x)
		f := &ast.Field{
			Label:    e.stringLabel(x.Label),
			Optional: token.NoSpace.Pos(),
		}

		e.setField(x.Label, f)

		f.Value = e.expr(env, x.Value)

		// extractDocs(nil)
		return f

	case *adt.BulkOptionalField:
		e.setDocs(x)
		// set bulk in frame.
		frame := e.frame(0)

		expr := e.expr(env, x.Filter)
		frame.labelExpr = expr // see astutil.Resolve.

		if x.Label != 0 {
			expr = &ast.Alias{Ident: e.ident(x.Label), Expr: expr}
		}
		f := &ast.Field{
			Label: ast.NewList(expr),
		}

		frame.field = f

		if alias := aliasFromLabel(x.Src); alias != "" {
			frame.dynamicFields = append(frame.dynamicFields, &entry{
				alias: alias,
				field: f,
			})
		}

		f.Value = e.expr(env, x.Value)

		return f

	case *adt.DynamicField:
		e.setDocs(x)
		srcKey := x.Key

		f := &ast.Field{}

		key := e.expr(env, srcKey)
		switch key.(type) {
		case *ast.Interpolation, *ast.BasicLit:
		default:
			key = &ast.ParenExpr{X: key}
		}
		f.Label = key.(ast.Label)

		alias := aliasFromLabel(x.Src)

		frame := e.frame(0)
		frame.dynamicFields = append(frame.dynamicFields, &entry{
			alias: alias,
			field: f,
		})
		// extractDocs(nil)

		f.Value = e.expr(env, x.Value)

		return f

	default:
		panic(fmt.Sprintf("unknown field %T", x))
	}
}

func (e *exporter) setField(label adt.Feature, f *ast.Field) {
	frame := e.frame(0)
	entry := frame.fields[label]
	entry.field = f
	entry.node = f.Value
	frame.fields[label] = entry
}

func aliasFromLabel(src *ast.Field) string {
	if src != nil {
		if a, ok := src.Label.(*ast.Alias); ok {
			return a.Ident.Name
		}
	}
	return ""
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

func (e *exporter) comprehension(env *adt.Environment, comp *adt.Comprehension) *ast.Comprehension {
	c := &ast.Comprehension{}

	y := comp.Clauses

loop:
	for {
		switch x := y.(type) {
		case *adt.ForClause:
			env := &adt.Environment{Up: env, Vertex: empty}
			value := e.ident(x.Value)
			clause := &ast.ForClause{
				Value:  value,
				Source: e.expr(env, x.Src),
			}
			c.Clauses = append(c.Clauses, clause)

			_, saved := e.pushFrame(empty, nil)
			defer e.popFrame(saved)

			if x.Key != adt.InvalidLabel ||
				(x.Syntax != nil && x.Syntax.Key != nil) {
				key := e.ident(x.Key)
				clause.Key = key
				e.addField(x.Key, nil, clause)
			}
			e.addField(x.Value, nil, clause)

			y = x.Dst

		case *adt.IfClause:
			clause := &ast.IfClause{Condition: e.expr(env, x.Condition)}
			c.Clauses = append(c.Clauses, clause)
			y = x.Dst

		case *adt.LetClause:
			env := &adt.Environment{Up: env, Vertex: empty}
			clause := &ast.LetClause{
				Ident: e.ident(x.Label),
				Expr:  e.expr(env, x.Expr),
			}
			c.Clauses = append(c.Clauses, clause)

			_, saved := e.pushFrame(empty, nil)
			defer e.popFrame(saved)

			e.addField(x.Label, nil, clause)

			y = x.Dst

		case *adt.ValueClause:
			break loop

		default:
			panic(fmt.Sprintf("unknown field %T", x))
		}
	}

	v := e.expr(env, adt.ToExpr(comp.Value))
	if _, ok := v.(*ast.StructLit); !ok {
		v = ast.NewStruct(ast.Embed(v))
	}
	c.Value = v
	return c
}
