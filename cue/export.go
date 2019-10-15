// Copyright 2018 The CUE Authors
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

package cue

import (
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
	"github.com/cockroachdb/apd/v2"
)

func doEval(m options) bool {
	return !m.raw
}

func export(ctx *context, v value, m options) (n ast.Node, imports []string) {
	e := exporter{ctx, m, nil, map[label]bool{}, map[string]importInfo{}, false}
	top, ok := v.evalPartial(ctx).(*structLit)
	if ok {
		top, err := top.expandFields(ctx)
		if err != nil {
			v = err
		} else {
			for _, a := range top.arcs {
				e.top[a.feature] = true
			}
		}
	}

	value := e.expr(v)
	if len(e.imports) == 0 {
		// TODO: unwrap structs?
		return value, nil
	}
	imports = make([]string, 0, len(e.imports))
	for k := range e.imports {
		imports = append(imports, k)
	}
	sort.Strings(imports)

	importDecl := &ast.ImportDecl{}
	file := &ast.File{Decls: []ast.Decl{importDecl}}

	for _, k := range imports {
		info := e.imports[k]
		ident := (*ast.Ident)(nil)
		if info.name != "" {
			ident = ast.NewIdent(info.name)
		}
		if info.alias != "" {
			file.Decls = append(file.Decls, &ast.Alias{
				Ident: ast.NewIdent(info.alias),
				Expr:  ast.NewIdent(info.short),
			})
		}
		importDecl.Specs = append(importDecl.Specs, ast.NewImport(ident, k))
	}

	if obj, ok := value.(*ast.StructLit); ok {
		file.Decls = append(file.Decls, obj.Elts...)
	} else {
		file.Decls = append(file.Decls, &ast.EmbedDecl{Expr: value})
	}

	// resolve the file.
	return file, imports
}

type exporter struct {
	ctx     *context
	mode    options
	stack   []remap
	top     map[label]bool        // label to alias or ""
	imports map[string]importInfo // pkg path to info
	inDef   bool                  // TODO(recclose):use count instead
}

type importInfo struct {
	name  string
	short string
	alias string
}

type remap struct {
	key  scope // structLit or params
	from label
	to   *ast.Ident
	syn  *ast.StructLit
}

func (p *exporter) unique(s string) string {
	s = strings.ToUpper(s)
	lab := s
	for {
		if _, ok := p.ctx.findLabel(lab); !ok {
			p.ctx.label(lab, true)
			break
		}
		lab = s + fmt.Sprintf("%0.6x", rand.Intn(1<<24))
	}
	return lab
}

func (p *exporter) label(f label) ast.Label {
	orig := p.ctx.labelStr(f)
	str := strconv.Quote(orig)
	if len(orig)+2 < len(str) || (strings.HasPrefix(orig, "_") && f&1 == 0) {
		return &ast.BasicLit{Value: str}
	}
	for i, r := range orig {
		if unicode.IsLetter(r) || r == '_' {
			continue
		}
		if i > 0 && unicode.IsDigit(r) {
			continue
		}
		return &ast.BasicLit{Value: str}
	}
	return &ast.Ident{Name: orig}
}

func (p *exporter) identifier(f label) *ast.Ident {
	str := p.ctx.labelStr(f)
	return &ast.Ident{Name: str}
}

func (p *exporter) ident(str string) *ast.Ident {
	return &ast.Ident{Name: str}
}

func (p *exporter) clause(v value) (n ast.Clause, next yielder) {
	switch x := v.(type) {
	case *feed:
		feed := &ast.ForClause{
			Value:  p.identifier(x.fn.params.arcs[1].feature),
			Source: p.expr(x.source),
		}
		key := x.fn.params.arcs[0]
		if p.ctx.labelStr(key.feature) != "_" {
			feed.Key = p.identifier(key.feature)
		}
		return feed, x.fn.value.(yielder)

	case *guard:
		return &ast.IfClause{Condition: p.expr(x.condition)}, x.value
	}
	panic(fmt.Sprintf("unsupported clause type %T", v))
}

func (p *exporter) shortName(inst *Instance, preferred, pkg string) string {
	info, ok := p.imports[pkg]
	short := info.short
	if !ok {
		short = inst.Name
		if _, ok := p.top[p.ctx.label(short, true)]; ok && preferred != "" {
			short = preferred
			info.name = short
		}
		for {
			if _, ok := p.top[p.ctx.label(short, true)]; !ok {
				break
			}
			short += "x"
			info.name = short
		}
		info.short = short
		p.top[p.ctx.label(short, true)] = true
		p.imports[pkg] = info
	}
	f := p.ctx.label(short, true)
	for _, e := range p.stack {
		if e.from == f {
			if info.alias == "" {
				info.alias = p.unique(short)
				p.imports[pkg] = info
			}
			short = info.alias
			break
		}
	}
	return short
}

func hasTemplate(s *ast.StructLit) bool {
	for _, e := range s.Elts {
		if f, ok := e.(*ast.Field); ok {
			if _, ok := f.Label.(*ast.TemplateLabel); ok {
				return true
			}
		}
	}
	return false
}

func (p *exporter) showOptional() bool {
	return !p.mode.omitOptional && !p.mode.concrete
}

func (p *exporter) closeOrOpen(s *ast.StructLit, isClosed bool) ast.Expr {
	// Note, there is no point in printing close if we are dropping optional
	// fields, as by this the meaning of close will change anyway.
	if !p.showOptional() {
		return s
	}
	if isClosed && !p.inDef {
		return ast.NewCall(ast.NewIdent("close"), s)
	}
	if !isClosed && p.inDef && !hasTemplate(s) {
		s.Elts = append(s.Elts, &ast.Ellipsis{})
	}
	return s
}

func (p *exporter) isComplete(v value, all bool) bool {
	switch x := v.(type) {
	case *numLit, *stringLit, *bytesLit, *nullLit, *boolLit:
		return true
	case *list:
		return true
	case *structLit:
		return !all
	case *bottom:
		return !isIncomplete(x)
	case *closeIfStruct:
		return p.isComplete(x.value, all)
	}
	return false
}

func isDisjunction(v value) bool {
	switch x := v.(type) {
	case *disjunction:
		return true
	case *closeIfStruct:
		return isDisjunction(x.value)
	}
	return false
}

func (p *exporter) recExpr(v value, e evaluated, optional bool) ast.Expr {
	m := p.ctx.manifest(e)
	if optional || (!p.isComplete(m, false) && (!p.mode.concrete)) {
		// TODO: do something more principled than this hack.
		// This likely requires disjunctions to keep track of original
		// values (so using arcs instead of values).
		p := &exporter{p.ctx, options{concrete: true, raw: true}, p.stack, p.top, p.imports, p.inDef}
		if isDisjunction(v) || isBottom(e) {
			return p.expr(v)
		}
		return p.expr(e)
	}
	return p.expr(e)
}

func (p *exporter) isClosed(x *structLit) bool {
	return x.closeStatus.shouldClose()
}

func (p *exporter) expr(v value) ast.Expr {
	// TODO: use the raw expression for convert incomplete errors downstream
	// as well.
	if doEval(p.mode) || p.mode.concrete {
		e := v.evalPartial(p.ctx)
		x := p.ctx.manifest(e)

		if !p.isComplete(x, true) {
			if isBottom(e) {
				p = &exporter{p.ctx, options{raw: true}, p.stack, p.top, p.imports, p.inDef}
				return p.expr(v)
			}
			if doEval(p.mode) {
				v = e
			}
		} else {
			v = x
		}
	}

	old := p.stack
	defer func() { p.stack = old }()

	// TODO: also add position information.
	switch x := v.(type) {
	case *builtin:
		if x.pkg == 0 {
			return ast.NewIdent(x.Name)
		}
		pkg := p.ctx.labelStr(x.pkg)
		inst := builtins[pkg]
		short := p.shortName(inst, "", pkg)
		return ast.NewSel(ast.NewIdent(short), x.Name)

	case *nodeRef:
		if x.label == 0 {
			// NOTE: this nodeRef is used within a selector.
			return nil
		}
		short := p.ctx.labelStr(x.label)

		if inst := p.ctx.getImportFromNode(x.node); inst != nil {
			return ast.NewIdent(p.shortName(inst, short, inst.ImportPath))
		}

		// fix shadowed label.
		return ast.NewIdent(short)

	case *selectorExpr:
		n := p.expr(x.x)
		if n != nil {
			return ast.NewSel(n, p.ctx.labelStr(x.feature))
		}
		ident := p.identifier(x.feature)
		node, ok := x.x.(*nodeRef)
		if !ok {
			// TODO: should not happen: report error
			return ident
		}
		// TODO: nodes may have changed. Use different algorithm.
		conflict := false
		for i := len(p.stack) - 1; i >= 0; i-- {
			e := &p.stack[i]
			if e.from != x.feature {
				continue
			}
			if e.key != node.node {
				conflict = true
				continue
			}
			if conflict {
				ident = e.to
				if e.to == nil {
					name := p.unique(p.ctx.labelStr(x.feature))
					e.syn.Elts = append(e.syn.Elts, &ast.Alias{
						Ident: p.ident(name),
						Expr:  p.identifier(x.feature),
					})
					ident = p.ident(name)
					e.to = ident
				}
			}
			return ident
		}
		// TODO: should not happen: report error
		return ident

	case *indexExpr:
		return &ast.IndexExpr{X: p.expr(x.x), Index: p.expr(x.index)}

	case *sliceExpr:
		return &ast.SliceExpr{
			X:    p.expr(x.x),
			Low:  p.expr(x.lo),
			High: p.expr(x.hi),
		}

	case *callExpr:
		call := &ast.CallExpr{}
		b := x.x.evalPartial(p.ctx)
		if b, ok := b.(*builtin); ok {
			call.Fun = p.expr(b)
		} else {
			call.Fun = p.expr(x.x)
		}
		for _, a := range x.args {
			call.Args = append(call.Args, p.expr(a))
		}
		return call

	case *customValidator:
		call := ast.NewCall(p.expr(x.call))
		for _, a := range x.args {
			call.Args = append(call.Args, p.expr(a))
		}
		return call

	case *unaryExpr:
		return &ast.UnaryExpr{Op: opMap[x.op], X: p.expr(x.x)}

	case *binaryExpr:
		// opUnifyUnchecked: represented as embedding. The two arguments must
		// be structs.
		if x.op == opUnifyUnchecked {
			s := &ast.StructLit{}
			return p.closeOrOpen(s, p.embedding(s, x))
		}
		return &ast.BinaryExpr{
			X:  p.expr(x.left),
			Op: opMap[x.op], Y: p.expr(x.right),
		}

	case *bound:
		return &ast.UnaryExpr{Op: opMap[x.op], X: p.expr(x.value)}

	case *unification:
		b := boundSimplifier{p: p}
		vals := make([]evaluated, 0, 3)
		for _, v := range x.values {
			if !b.add(v) {
				vals = append(vals, v)
			}
		}
		e := b.expr(p.ctx)
		for _, v := range vals {
			e = wrapBin(e, p.expr(v), opUnify)
		}
		return e

	case *disjunction:
		if len(x.values) == 1 {
			return p.expr(x.values[0].val)
		}
		expr := func(v dValue) ast.Expr {
			e := p.expr(v.val)
			if v.marked {
				e = &ast.UnaryExpr{Op: token.MUL, X: e}
			}
			return e
		}
		bin := expr(x.values[0])
		for _, v := range x.values[1:] {
			bin = &ast.BinaryExpr{X: bin, Op: token.OR, Y: expr(v)}
		}
		return bin

	case *closeIfStruct:
		return p.expr(x.value)

	case *structLit:
		st, err := p.structure(x, !p.isClosed(x))
		if err != nil {
			return p.expr(err)
		}
		expr := p.closeOrOpen(st, p.isClosed(x))
		switch {
		// If a template is non-nil, we only show it if printing of
		// optional fields is requested. If a struct is not closed it was
		// already generated before. Furthermore, if if we are in evaluation
		// mode, the struct is already unified, so there is no need to print it.
		case x.template != nil && p.showOptional() && p.isClosed(x) && !doEval(p.mode):
			l, ok := x.template.evalPartial(p.ctx).(*lambdaExpr)
			if !ok {
				break
			}
			v := l.value
			if c, ok := v.(*closeIfStruct); ok {
				v = c.value
			}
			if _, ok := v.(*top); ok {
				break
			}
			expr = &ast.BinaryExpr{X: expr, Op: token.AND, Y: &ast.StructLit{
				Elts: []ast.Decl{&ast.Field{
					Label: &ast.TemplateLabel{
						Ident: p.identifier(l.params.arcs[0].feature),
					},
					Value: p.expr(l.value),
				}},
			}}
		}
		return expr

	case *fieldComprehension:
		panic("should be handled in structLit")

	case *listComprehension:
		var clauses []ast.Clause
		for y, next := p.clause(x.clauses); ; y, next = p.clause(next) {
			clauses = append(clauses, y)
			if yield, ok := next.(*yield); ok {
				return &ast.ListComprehension{
					Expr:    p.expr(yield.value),
					Clauses: clauses,
				}
			}
		}

	case *nullLit:
		return p.ident("null")

	case *boolLit:
		return p.ident(fmt.Sprint(x.b))

	case *stringLit:
		return &ast.BasicLit{
			Kind:  token.STRING,
			Value: quote(x.str, '"'),
		}

	case *bytesLit:
		return &ast.BasicLit{
			Kind:  token.STRING,
			Value: quote(string(x.b), '\''),
		}

	case *numLit:
		if x.k&intKind != 0 {
			return &ast.BasicLit{
				Kind:  token.INT,
				Value: x.v.Text('f'),
			}
		}
		return &ast.BasicLit{
			Kind:  token.FLOAT,
			Value: x.v.Text('g'),
		}

	case *durationLit:
		panic("unimplemented")

	case *interpolation:
		t := &ast.Interpolation{}
		multiline := false
		// TODO: mark formatting in interpolation itself.
		for i := 0; i < len(x.parts); i += 2 {
			str := x.parts[i].(*stringLit).str
			if strings.IndexByte(str, '\n') >= 0 {
				multiline = true
				break
			}
		}
		quote := `"`
		if multiline {
			quote = `"""`
		}
		prefix := quote
		suffix := `\(`
		for i, e := range x.parts {
			if i%2 == 1 {
				t.Elts = append(t.Elts, p.expr(e))
			} else {
				buf := []byte(prefix)
				if i == len(x.parts)-1 {
					suffix = quote
				}
				str := e.(*stringLit).str
				if multiline {
					buf = appendEscapeMulti(buf, str, '"')
				} else {
					buf = appendEscaped(buf, str, '"', true)
				}
				buf = append(buf, suffix...)
				t.Elts = append(t.Elts, &ast.BasicLit{
					Kind:  token.STRING,
					Value: string(buf),
				})
			}
			prefix = ")"
		}
		return t

	case *list:
		list := &ast.ListLit{}
		var expr ast.Expr = list
		for i, a := range x.elem.arcs {
			if !doEval(p.mode) {
				list.Elts = append(list.Elts, p.expr(a.v))
			} else {
				e := x.elem.at(p.ctx, i)
				list.Elts = append(list.Elts, p.recExpr(a.v, e, false))
			}
		}
		max := maxNum(x.len)
		num, ok := max.(*numLit)
		if !ok {
			min := minNum(x.len)
			num, _ = min.(*numLit)
		}
		ln := 0
		if num != nil {
			x, _ := num.v.Int64()
			ln = int(x)
		}
		open := false
		switch max.(type) {
		case *top, *basicType:
			open = true
		}
		if !ok || ln > len(x.elem.arcs) {
			list.Elts = append(list.Elts, &ast.Ellipsis{Type: p.expr(x.typ)})
			if !open && !isTop(x.typ) {
				expr = &ast.BinaryExpr{
					X: &ast.BinaryExpr{
						X:  p.expr(x.len),
						Op: token.MUL,
						Y: &ast.ListLit{Elts: []ast.Expr{
							p.expr(x.typ),
						}},
					},
					Op: token.AND,
					Y:  list,
				}

			}
		}
		return expr

	case *bottom:
		err := &ast.BottomLit{}
		if x.format != "" {
			comment := &ast.Comment{Text: "// " + x.msg()}
			err.AddComment(&ast.CommentGroup{
				Line:     true,
				Position: 2,
				List:     []*ast.Comment{comment},
			})
		}
		return err

	case *top:
		return p.ident("_")

	case *basicType:
		return p.ident(x.k.String())

	case *lambdaExpr:
		return p.ident("TODO: LAMBDA")

	default:
		panic(fmt.Sprintf("unimplemented type %T", x))
	}
}

func (p *exporter) structure(x *structLit, addTempl bool) (ret *ast.StructLit, err *bottom) {
	obj := &ast.StructLit{}
	if doEval(p.mode) {
		x, err = x.expandFields(p.ctx)
		if err != nil {
			return nil, err
		}
	}

	for _, a := range x.arcs {
		p.stack = append(p.stack, remap{
			key:  x,
			from: a.feature,
			to:   nil,
			syn:  obj,
		})
	}
	if x.emit != nil {
		obj.Elts = append(obj.Elts, &ast.EmbedDecl{Expr: p.expr(x.emit)})
	}
	switch {
	case x.template != nil && p.showOptional() && addTempl:
		l, ok := x.template.evalPartial(p.ctx).(*lambdaExpr)
		if ok {
			if _, ok := l.value.(*top); ok && !p.isClosed(x) {
				break
			}
			obj.Elts = append(obj.Elts, &ast.Field{
				Label: &ast.TemplateLabel{
					Ident: p.identifier(l.params.arcs[0].feature),
				},
				Value: p.expr(l.value),
			})
		} // TODO: else record error
	}
	for i, a := range x.arcs {
		f := &ast.Field{
			Label: p.label(a.feature),
		}
		// TODO: allow the removal of hidden fields. However, hidden fields
		// that still used in incomplete expressions should not be removed
		// (unless RequireConcrete is requested).
		if a.optional {
			// Optional fields are almost never concrete. We omit them in
			// concrete mode to allow the user to use the -a option in eval
			// without getting many errors.
			if p.mode.omitOptional || p.mode.concrete {
				continue
			}
			f.Optional = token.NoSpace.Pos()
		}
		if a.definition {
			if p.mode.omitDefinitions || p.mode.concrete {
				continue
			}
			f.Token = token.ISA
		}
		if a.feature&hidden != 0 && p.mode.concrete && p.mode.omitHidden {
			continue
		}
		oldInDef := p.inDef
		p.inDef = a.definition || p.inDef
		if !doEval(p.mode) {
			f.Value = p.expr(a.v)
		} else {
			f.Value = p.recExpr(a.v, x.at(p.ctx, i), a.optional)
		}
		p.inDef = oldInDef
		if a.attrs != nil && !p.mode.omitAttrs {
			for _, at := range a.attrs.attr {
				f.Attrs = append(f.Attrs, &ast.Attribute{Text: at.text})
			}
		}
		obj.Elts = append(obj.Elts, f)
	}

	for _, v := range x.comprehensions {
		switch c := v.(type) {
		case *fieldComprehension:
			l := p.expr(c.key)
			label, _ := l.(ast.Label)
			opt := token.NoPos
			if c.opt {
				opt = token.NoSpace.Pos() // anything but token.NoPos
			}
			tok := token.COLON
			if c.def {
				tok = token.ISA
			}
			f := &ast.Field{
				Label:    label,
				Optional: opt,
				Token:    tok,
				Value:    p.expr(c.val),
			}
			obj.Elts = append(obj.Elts, f)

		case *structComprehension:
			var clauses []ast.Clause
			next := c.clauses
			for {
				if yield, ok := next.(*yield); ok {
					obj.Elts = append(obj.Elts, &ast.Comprehension{
						Clauses: clauses,
						Value:   p.expr(yield.value),
					})
					break
				}

				var y ast.Clause
				y, next = p.clause(next)
				clauses = append(clauses, y)
			}
		}
	}
	return obj, nil
}

func (p *exporter) embedding(s *ast.StructLit, n value) (closed bool) {
	switch x := n.(type) {
	case *structLit:
		st, err := p.structure(x, true)
		if err != nil {
			n = err
			break
		}
		s.Elts = append(s.Elts, st.Elts...)
		return p.isClosed(x)

	case *binaryExpr:
		if x.op != opUnifyUnchecked {
			// should not happen
			s.Elts = append(s.Elts, &ast.EmbedDecl{Expr: p.expr(x)})
			return false
		}
		leftClosed := p.embedding(s, x.left)
		rightClosed := p.embedding(s, x.right)
		return leftClosed || rightClosed
	}
	s.Elts = append(s.Elts, &ast.EmbedDecl{Expr: p.expr(n)})
	return false
}

// quote quotes the given string.
func quote(str string, quote byte) string {
	if strings.IndexByte(str, '\n') < 0 {
		buf := []byte{quote}
		buf = appendEscaped(buf, str, quote, true)
		buf = append(buf, quote)
		return string(buf)
	}
	buf := []byte{quote, quote, quote}
	buf = append(buf, multiSep...)
	buf = appendEscapeMulti(buf, str, quote)
	buf = append(buf, quote, quote, quote)
	return string(buf)
}

// TODO: consider the best indent strategy.
const multiSep = "\n        "

func appendEscapeMulti(buf []byte, str string, quote byte) []byte {
	// TODO(perf)
	a := strings.Split(str, "\n")
	for _, s := range a {
		buf = appendEscaped(buf, s, quote, true)
		buf = append(buf, multiSep...)
	}
	return buf
}

const lowerhex = "0123456789abcdef"

func appendEscaped(buf []byte, s string, quote byte, graphicOnly bool) []byte {
	for width := 0; len(s) > 0; s = s[width:] {
		r := rune(s[0])
		width = 1
		if r >= utf8.RuneSelf {
			r, width = utf8.DecodeRuneInString(s)
		}
		if width == 1 && r == utf8.RuneError {
			buf = append(buf, `\x`...)
			buf = append(buf, lowerhex[s[0]>>4])
			buf = append(buf, lowerhex[s[0]&0xF])
			continue
		}
		buf = appendEscapedRune(buf, r, quote, graphicOnly)
	}
	return buf
}

func appendEscapedRune(buf []byte, r rune, quote byte, graphicOnly bool) []byte {
	var runeTmp [utf8.UTFMax]byte
	if r == rune(quote) || r == '\\' { // always backslashed
		buf = append(buf, '\\')
		buf = append(buf, byte(r))
		return buf
	}
	// TODO(perf): IsGraphic calls IsPrint.
	if strconv.IsPrint(r) || graphicOnly && strconv.IsGraphic(r) {
		n := utf8.EncodeRune(runeTmp[:], r)
		buf = append(buf, runeTmp[:n]...)
		return buf
	}
	switch r {
	case '\a':
		buf = append(buf, `\a`...)
	case '\b':
		buf = append(buf, `\b`...)
	case '\f':
		buf = append(buf, `\f`...)
	case '\n':
		buf = append(buf, `\n`...)
	case '\r':
		buf = append(buf, `\r`...)
	case '\t':
		buf = append(buf, `\t`...)
	case '\v':
		buf = append(buf, `\v`...)
	default:
		switch {
		case r < ' ':
			// Invalid for strings, only bytes.
			buf = append(buf, `\x`...)
			buf = append(buf, lowerhex[byte(r)>>4])
			buf = append(buf, lowerhex[byte(r)&0xF])
		case r > utf8.MaxRune:
			r = 0xFFFD
			fallthrough
		case r < 0x10000:
			buf = append(buf, `\u`...)
			for s := 12; s >= 0; s -= 4 {
				buf = append(buf, lowerhex[r>>uint(s)&0xF])
			}
		default:
			buf = append(buf, `\U`...)
			for s := 28; s >= 0; s -= 4 {
				buf = append(buf, lowerhex[r>>uint(s)&0xF])
			}
		}
	}
	return buf
}

type boundSimplifier struct {
	p *exporter

	isInt  bool
	min    *bound
	minNum *numLit
	max    *bound
	maxNum *numLit
}

func (s *boundSimplifier) add(v value) (used bool) {
	switch x := v.(type) {
	case *basicType:
		switch x.k & scalarKinds {
		case intKind:
			s.isInt = true
			return true
		}

	case *bound:
		if x.k&concreteKind == intKind {
			s.isInt = true
		}
		switch x.op {
		case opGtr:
			if n, ok := x.value.(*numLit); ok {
				if s.min == nil || s.minNum.v.Cmp(&n.v) != 1 {
					s.min = x
					s.minNum = n
				}
				return true
			}

		case opGeq:
			if n, ok := x.value.(*numLit); ok {
				if s.min == nil || s.minNum.v.Cmp(&n.v) == -1 {
					s.min = x
					s.minNum = n
				}
				return true
			}

		case opLss:
			if n, ok := x.value.(*numLit); ok {
				if s.max == nil || s.maxNum.v.Cmp(&n.v) != -1 {
					s.max = x
					s.maxNum = n
				}
				return true
			}

		case opLeq:
			if n, ok := x.value.(*numLit); ok {
				if s.max == nil || s.maxNum.v.Cmp(&n.v) == 1 {
					s.max = x
					s.maxNum = n
				}
				return true
			}
		}
	}

	return false
}

type builtinRange struct {
	typ string
	lo  *apd.Decimal
	hi  *apd.Decimal
}

func makeDec(s string) *apd.Decimal {
	d, _, err := apd.NewFromString(s)
	if err != nil {
		panic(err)
	}
	return d
}

func (s *boundSimplifier) expr(ctx *context) (e ast.Expr) {
	if s.min == nil || s.max == nil {
		return nil
	}
	switch {
	case s.isInt:
		t := s.matchRange(intRanges)
		if t != "" {
			e = ast.NewIdent(t)
			break
		}
		if sign := s.minNum.v.Sign(); sign == -1 {
			e = ast.NewIdent("int")

		} else {
			e = ast.NewIdent("uint")
			if sign == 0 && s.min.op == opGeq {
				s.min = nil
				break
			}
		}
		fallthrough
	default:
		t := s.matchRange(floatRanges)
		if t != "" {
			e = wrapBin(e, ast.NewIdent(t), opUnify)
		}
	}

	if s.min != nil {
		e = wrapBin(e, s.p.expr(s.min), opUnify)
	}
	if s.max != nil {
		e = wrapBin(e, s.p.expr(s.max), opUnify)
	}
	return e
}

func (s *boundSimplifier) matchRange(ranges []builtinRange) (t string) {
	for _, r := range ranges {
		if !s.minNum.v.IsZero() && s.min.op == opGeq && s.minNum.v.Cmp(r.lo) == 0 {
			switch s.maxNum.v.Cmp(r.hi) {
			case 0:
				if s.max.op == opLeq {
					s.max = nil
				}
				s.min = nil
				return r.typ
			case -1:
				if !s.minNum.v.IsZero() {
					s.min = nil
					return r.typ
				}
			case 1:
			}
		} else if s.max.op == opLeq && s.maxNum.v.Cmp(r.hi) == 0 {
			switch s.minNum.v.Cmp(r.lo) {
			case -1:
			case 0:
				if s.min.op == opGeq {
					s.min = nil
				}
				fallthrough
			case 1:
				s.max = nil
				return r.typ
			}
		}
	}
	return ""
}

var intRanges = []builtinRange{
	{"int8", makeDec("-128"), makeDec("127")},
	{"int16", makeDec("-32768"), makeDec("32767")},
	{"int32", makeDec("-2147483648"), makeDec("2147483647")},
	{"int64", makeDec("-9223372036854775808"), makeDec("9223372036854775807")},
	{"int128", makeDec("-170141183460469231731687303715884105728"),
		makeDec("170141183460469231731687303715884105727")},

	{"uint8", makeDec("0"), makeDec("255")},
	{"uint16", makeDec("0"), makeDec("65535")},
	{"uint32", makeDec("0"), makeDec("4294967295")},
	{"uint64", makeDec("0"), makeDec("18446744073709551615")},
	{"uint128", makeDec("0"), makeDec("340282366920938463463374607431768211455")},

	// {"rune", makeDec("0"), makeDec(strconv.Itoa(0x10FFFF))},
}

var floatRanges = []builtinRange{
	// 2**127 * (2**24 - 1) / 2**23
	{"float32",
		makeDec("-3.40282346638528859811704183484516925440e+38"),
		makeDec("+3.40282346638528859811704183484516925440e+38")},

	// 2**1023 * (2**53 - 1) / 2**52
	{"float64",
		makeDec("-1.797693134862315708145274237317043567981e+308"),
		makeDec("+1.797693134862315708145274237317043567981e+308")},
}

func wrapBin(a, b ast.Expr, op op) ast.Expr {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	return &ast.BinaryExpr{
		X:  a,
		Op: opMap[op],
		Y:  b,
	}
}
