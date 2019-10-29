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
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

func debugStr(ctx *context, v value) string {
	p := newPrinter(ctx)
	p.showNodeRef = true
	p.str(v)
	return p.w.String()
}

func (c *context) str(v value) string {
	p := newPrinter(c)
	p.str(v)
	return p.w.String()
}

func (c *context) ref(v scope) string {
	v = c.deref(v)
	if c.nodeRefs == nil {
		c.nodeRefs = map[scope]string{}
	}
	ref, ok := c.nodeRefs[v]
	if ok {
		return ref
	}
	ref = strconv.Itoa(len(c.nodeRefs))
	c.nodeRefs[v] = ref
	return ref
}

func (c *context) indent() {
	fmt.Print(strings.Repeat("    ", c.level))
}

func (c *context) debugPrint(args ...interface{}) {
	if c.trace {
		c.indent()
		c.println(args...)
	}
}

func (c *context) println(args ...interface{}) {
	for i, a := range args {
		if i != 0 {
			fmt.Print(" ")
		}
		switch x := a.(type) {
		case value:
			fmt.Print(debugStr(c, x))
		default:
			fmt.Print(x)
		}
	}
	fmt.Println()
}

// func trace(c *context, r rewriter, n *node) (*context, rewriter, *node) {
// 	n = derefNode(n)
// 	name := "evaluate"
// 	if r != nil {
// 		name = fmt.Sprintf("%T", r)
// 	}
// 	c.debugPrint("---", name, c.ref(n))
// 	if n.obj != nil {
// 		c.debugPrint("<<< node: ", debugStr(c, n.obj))
// 	}
// 	if n.expr != nil {
// 		c.debugPrint("<<< expr: ", debugStr(c, n.expr))
// 	}
// 	if n.value != nil {
// 		c.debugPrint("<<< value:", debugStr(c, n.value))
// 	}
// 	c.level++
// 	return c, r, n
// }

// func un(c *context, r rewriter, n *node) {
// 	n = derefNode(n)
// 	c.level--
// 	if n.expr != nil {
// 		c.debugPrint(">>> expr:", debugStr(c, n.expr))
// 	}
// 	if n.value != nil {
// 		c.debugPrint(">>> value:", debugStr(c, n.value))
// 	}
// 	if n.obj != nil {
// 		c.debugPrint(">>> node: ", debugStr(c, n.obj))
// 	}
// }

func indent(c *context, msg string, x value) (_ *context, m, v string) {
	str := debugStr(c, x)
	c.debugPrint("...", msg)
	c.level++
	c.debugPrint("in:", str)
	return c, msg, str
}

func uni(c *context, msg, oldValue string) {
	c.debugPrint("was:   ", oldValue)
	c.level--
	c.debugPrint("...", msg)
}

func newPrinter(ctx *context) *printer {
	return &printer{
		ctx: ctx,
		w:   &bytes.Buffer{},
	}
}

type printer struct {
	ctx         *context
	w           *bytes.Buffer
	showNodeRef bool
}

func (p *printer) label(f label) string {
	if p.ctx == nil {
		return strconv.Itoa(int(f))
	}
	return p.ctx.labelStr(f)
}

func (p *printer) writef(format string, args ...interface{}) {
	fmt.Fprintf(p.w, format, args...)
}

func (p *printer) write(args ...interface{}) {
	fmt.Fprint(p.w, args...)
}

func lambdaName(f label, v value) label {
	switch x := v.(type) {
	case *nodeRef:
		return lambdaName(f, x.node)
	case *lambdaExpr:
		if f == 0 && len(x.params.arcs) == 1 {
			return x.params.arcs[0].feature
		}
	}
	return f
}

func (p *printer) str(v interface{}) {
	writef := p.writef
	write := p.write
	switch x := v.(type) {
	case nil:
		write("*nil*")
	case string:
		write(x)
	case *builtin:
		write(x.name(p.ctx))
	case *nodeRef:
		if p.showNodeRef {
			writef("<%s>", p.ctx.ref(x.node))
		}
	case *selectorExpr:
		f := lambdaName(x.feature, x.x)
		if _, ok := x.x.(*nodeRef); ok && !p.showNodeRef {
			write(p.label(f))
		} else {
			p.str(x.x)
			writef(".%v", p.label(f))
		}
	case *indexExpr:
		p.str(x.x)
		write("[")
		p.str(x.index)
		write("]")
	case *sliceExpr:
		p.str(x.x)
		write("[")
		if x.lo != nil {
			p.str(x.lo)
		}
		write(":")
		if x.hi != nil {
			p.str(x.hi)
		}
		write("]")
	case *callExpr:
		p.str(x.x)
		write(" (")
		for i, a := range x.args {
			p.str(a)
			if i < len(x.args)-1 {
				write(",")
			}
		}
		write(")")
	case *customValidator:
		p.str(x.call)
		write(" (")
		for i, a := range x.args {
			p.str(a)
			if i < len(x.args)-1 {
				write(",")
			}
		}
		write(")")
	case *unaryExpr:
		write(x.op)
		p.str(x.x)
	case *binaryExpr:
		if x.op == opUnifyUnchecked {
			p.str(x.left)
			write(", ")
			p.str(x.right)
			break
		}
		write("(")
		p.str(x.left)
		writef(" %v ", x.op)
		p.str(x.right)
		write(")")
	case *unification:
		write("(")
		for i, v := range x.values {
			if i != 0 {
				writef(" & ")
			}
			p.str(v)
		}
		write(")")
	case *disjunction:
		write("(")
		for i, v := range x.values {
			if i != 0 {
				writef(" | ")
			}
			if v.marked {
				writef("*")
			}
			p.str(v.val)
		}
		write(")")
	case *lambdaExpr:
		if p.showNodeRef {
			writef("<%s>", p.ctx.ref(x))
		}
		write("(")
		p.str(x.params.arcs)
		write(")->")
		v := x.value
		// strip one layer of closeIf wrapper. Evaluation may cause one
		// layer to have not yet been evaluated. This is fine.
		if w, ok := v.(*closeIfStruct); ok {
			v = w.value
		}
		p.str(v)

	case *closeIfStruct:
		write("close(")
		p.str(x.value)
		write(")")

	case *optionals:
		if x == nil {
			break
		}
		wrap := func(v *optionals) {
			if x.closed.isClosed() {
				write("C{")
			}
			p.str(v)
			if x.closed.isClosed() {
				write("}")
			}
		}
		switch {
		case x.op == opUnify:
			write("(")
			wrap(x.left)
			write(" & ")
			wrap(x.right)
			write(")")

		case x.op == opUnifyUnchecked:
			wrap(x.left)
			write(", ")
			wrap(x.right)

		default:
			for i, t := range x.fields {
				if i > 0 {
					write(", ")
				}
				write("[")
				if t.key != nil {
					p.str(t.key)
				}
				write("]: ")
				p.str(t.value)
			}
		}

	case *structLit:
		if x == nil {
			write("*nil node*")
			break
		}
		if p.showNodeRef {
			p.writef("<%s>", p.ctx.ref(x))
		}
		if x.closeStatus.shouldClose() {
			write("C")
		}
		write("{")
		topDefault := x.optionals.isDotDotDot()
		if !topDefault && x.optionals != nil {
			p.str(x.optionals)
			write(", ")
		}

		if x.emit != nil {
			p.str(x.emit)
			write(", ")
		}
		p.str(x.arcs)
		for i, c := range x.comprehensions {
			p.str(c)
			if i < len(x.comprehensions)-1 {
				p.write(", ")
			}
		}
		if topDefault && !x.closeStatus.shouldClose() {
			if len(x.arcs) > 0 {
				p.write(", ")
			}
			p.write("...")
		}
		write("}")

	case []arc:
		for i, a := range x {
			p.str(a)

			if i < len(x)-1 {
				p.write(", ")
			}
		}

	case arc:
		n := x.v
		orig := p.label(x.feature)
		str := strconv.Quote(orig)
		if len(orig)+2 == len(str) {
			str = str[1 : len(str)-1]
		}
		p.writef(str)
		if x.optional {
			p.write("?")
		}
		if x.definition {
			p.write(" :: ")
		} else {
			p.write(": ")
		}
		p.str(n)
		if x.attrs != nil {
			for _, a := range x.attrs.attr {
				p.write(" ", a.text)
			}
		}

	case *fieldComprehension:
		p.str(x.key)
		writef(": ")
		p.str(x.val)

	case *listComprehension:
		writef("[")
		p.str(x.clauses)
		write(" ]")

	case *structComprehension:
		p.str(x.clauses)

	case *yield:
		writef(" yield ")
		p.str(x.value)

	case *feed:
		writef(" <%s>for ", p.ctx.ref(x.fn))
		a := x.fn.params.arcs[0]
		p.writef(p.label(a.feature))
		writef(", ")
		a = x.fn.params.arcs[1]
		p.writef(p.label(a.feature))
		writef(" in ")
		p.str(x.source)
		p.str(x.fn.value)

	case *guard:
		writef(" if ")
		p.str(x.condition)
		p.str(x.value)

	case *nullLit:
		write("null")
	case *boolLit:
		writef("%v", x.b)
	case *stringLit:
		writef("%q", x.str)
	case *bytesLit:
		str := strconv.Quote(string(x.b))
		str = str[1 : len(str)-1]
		writef("'%s'", str)
	case *numLit:
		if x.k&intKind != 0 {
			write(x.v.Text('f')) // also render info
		} else {
			write(x.v.Text('g')) // also render info
		}
	case *durationLit:
		write(x.d.String())
	case *bound:
		switch x.k & numKind {
		case intKind:
			p.writef("int & ")
		case floatKind:
			p.writef("float & ")
		}
		p.writef("%v", x.op)
		p.str(x.value)
	case *interpolation:
		for i, e := range x.parts {
			if i != 0 {
				write("+")
			}
			p.str(e)
		}
	case *list:
		// TODO: do not evaluate
		max := maxNum(x.len).evalPartial(p.ctx)
		inCast := false
		ellipsis := false
		n, ok := max.(*numLit)
		if !ok {
			// TODO: do not evaluate
			min := minNum(x.len).evalPartial(p.ctx)
			n, _ = min.(*numLit)
		}
		ln := 0
		if n != nil {
			x, _ := n.v.Int64()
			ln = int(x)
		}
		open := false
		switch max.(type) {
		case *top, *basicType:
			open = true
		}
		if !ok || ln > len(x.elem.arcs) {
			if !open && !isTop(x.typ) {
				p.str(x.len)
				write("*[")
				p.str(x.typ)
				write("]")
				if len(x.elem.arcs) == 0 {
					break
				}
				write("(")
				inCast = true
			}
			ellipsis = true
		}
		write("[")
		for i, a := range x.elem.arcs {
			p.str(a.v)
			if i < len(x.elem.arcs)-1 {
				write(",")
			}
		}
		if ellipsis {
			write(", ...")
			if !isTop(x.typ) {
				p.str(x.typ)
			}
		}
		write("]")
		if inCast {
			write(")")
		}

	case *bottom:
		write("_|_")
		if x.value != nil || x.format != "" {
			write("(")
			errs := x.sub
			if errs == nil {
				errs = []*bottom{x}
			}
			for i, x := range errs {
				if i > 0 {
					p.write(";")
				}
				if x.value != nil && p.showNodeRef {
					p.str(x.value)
					p.write(":")
				}
				write(x.msg())
			}
			write(")")
		}
	case *top:
		write("_") // ⊤
	case *basicType:
		write(x.k.String())

	default:
		panic(fmt.Sprintf("unimplemented type %T", x))
	}
}
