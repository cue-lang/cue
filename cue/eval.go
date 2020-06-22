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
)

type resolver interface {
	reference(ctx *context) value
}

var _ resolver = &selectorExpr{}
var _ resolver = &indexExpr{}

// decycleRef rewrites a reference that resolves to an evaluation cycle to
// an embedding that can be unified as is.
func decycleRef(ctx *context, v value) (value, scope) {
	switch x := v.(type) {
	case *selectorExpr:
		v, sc := decycleRef(ctx, x.X)
		if v == nil {
			e := x.evalPartial(ctx)
			v = e
			if cycleError(e) != nil {
				sc = &structLit{baseValue: x.base()}
				return &nodeRef{x.base(), sc, x.Sel}, sc
			}
			return nil, nil
		}
		return &selectorExpr{x.baseValue, v, x.Sel}, sc
	case *indexExpr:
		v, sc := decycleRef(ctx, x.X)
		if v == x {
			return nil, nil
		}
		return &indexExpr{x.baseValue, v, x.Index}, sc
	case *nodeRef:
		return nil, nil
	}
	return v, nil
}

func resolveReference(ctx *context, v value) evaluated {
	if r, ok := v.(resolver); ok {
		e := r.reference(ctx)
		if st, ok := e.(*structLit); ok {
			return st
		}
		if b, ok := e.(*bottom); ok {
			if b := cycleError(b); b != nil {
				// This is only called if we are unifying. The value referenced
				// is either a struct or not. In case the other value is not a
				// struct, we ensure an error by returning a struct. In case the
				// value is a struct, we postpone the evaluation of this
				// reference by creating an embedding for it (which are
				// evaluated after evaluating the struct itself.)
				if y, sc := decycleRef(ctx, v); y != v {
					st := &structLit{baseValue: v.base()}
					ctx.pushForwards(sc, st)
					cp := ctx.copy(y)
					ctx.popForwards()
					st.comprehensions = []compValue{{comp: cp}}
					return st
				}
				return b
			}
		}
	}
	return v.evalPartial(ctx)
}

func eval(idx *index, v value) evaluated {
	ctx := idx.newContext()
	return v.evalPartial(ctx)
}

func (x *nodeRef) evalPartial(ctx *context) (result evaluated) {
	return x.node.evalPartial(ctx)
}

// Atoms

func (x *top) evalPartial(ctx *context) evaluated    { return x }
func (x *bottom) evalPartial(ctx *context) evaluated { return x }

func (x *basicType) evalPartial(ctx *context) evaluated   { return x }
func (x *nullLit) evalPartial(ctx *context) evaluated     { return x }
func (x *boolLit) evalPartial(ctx *context) evaluated     { return x }
func (x *stringLit) evalPartial(ctx *context) evaluated   { return x }
func (x *bytesLit) evalPartial(ctx *context) evaluated    { return x }
func (x *numLit) evalPartial(ctx *context) evaluated      { return x }
func (x *durationLit) evalPartial(ctx *context) evaluated { return x }

func (x *lambdaExpr) evalPartial(ctx *context) evaluated {
	return ctx.deref(x).(*lambdaExpr)
}

func (x *selectorExpr) evalPartial(ctx *context) (result evaluated) {
	if ctx.trace {
		defer uni(indent(ctx, "selectorExpr", x))
		defer func() { ctx.debugPrint("result:", result) }()
	}

	e := newEval(ctx, true)

	const msgType = "invalid operation: %[5]s (type %[3]s does not support selection)"
	v := e.eval(x.X, structKind|lambdaKind, msgType, x)

	if e.is(v, structKind|lambdaKind, "") {
		sc, ok := v.(scope)
		if !ok {
			return ctx.mkErr(x, "invalid subject to selector (found %v)", v.Kind())
		}
		n := sc.Lookup(ctx, x.Sel)
		if n.optional {
			field := ctx.LabelStr(x.Sel)
			return ctx.mkErr(x, codeIncomplete, "field %q is optional", field)
		}
		if n.val() == nil {
			field := ctx.LabelStr(x.Sel)
			if st, ok := sc.(*structLit); ok && !st.isClosed() {
				return ctx.mkErr(x, codeIncomplete, "undefined field %q", field)
			}
			//	m.foo undefined (type map[string]bool has no field or method foo)
			// TODO: mention x.x in error message?
			return ctx.mkErr(x, "undefined field %q", field)
		}
		return n.Value
	}
	return e.err(&selectorExpr{x.baseValue, v, x.Sel})
}

func (x *selectorExpr) reference(ctx *context) (result value) {
	if ctx.trace {
		defer uni(indent(ctx, "selectorExpr", x))
		defer func() { ctx.debugPrint("result:", result) }()
	}

	e := newEval(ctx, true)

	const msgType = "invalid operation: %[5]s (type %[3]s does not support selection)"
	v := e.eval(x.X, structKind|lambdaKind, msgType, x)

	if e.is(v, structKind|lambdaKind, "") {
		sc, ok := v.(scope)
		if !ok {
			return ctx.mkErr(x, "invalid subject to selector (found %v)", v.Kind())
		}
		n := sc.Lookup(ctx, x.Sel)
		if n.optional {
			field := ctx.LabelStr(x.Sel)
			return ctx.mkErr(x, codeIncomplete, "field %q is optional", field)
		}
		if n.val() == nil {
			field := ctx.LabelStr(x.Sel)
			if st, ok := sc.(*structLit); ok && !st.isClosed() {
				return ctx.mkErr(x, codeIncomplete, "undefined field %q", field)
			}
			//	m.foo undefined (type map[string]bool has no field or method foo)
			// TODO: mention x.x in error message?
			return ctx.mkErr(x, "undefined field %q", field)
		}
		return n.v
	}
	return e.err(&selectorExpr{x.baseValue, v, x.Sel})
}

func (x *indexExpr) evalPartial(ctx *context) (result evaluated) {
	if ctx.trace {
		defer uni(indent(ctx, "indexExpr", x))
		defer func() { ctx.debugPrint("result:", result) }()
	}

	e := newEval(ctx, true)

	const msgType = "invalid operation: %[5]s (type %[3]s does not support indexing)"
	const msgIndexType = "invalid %[5]s index %[1]s (type %[3]s)"

	val := e.eval(x.X, listKind|structKind, msgType, x)
	k := val.Kind()
	index := e.eval(x.Index, stringKind|intKind, msgIndexType, k)

	switch v := val.(type) {
	case *structLit:
		if e.is(index, stringKind, msgIndexType, k) {
			s := index.strValue()
			// TODO: must lookup
			n := v.Lookup(ctx, ctx.StrLabel(s))
			if n.definition {
				return ctx.mkErr(x, index,
					"field %q is a definition", s)
			}
			if n.optional {
				return ctx.mkErr(x, index, codeIncomplete, "field %q is optional", s)
			}
			if n.val() == nil {
				if !v.isClosed() {
					return ctx.mkErr(x, index, codeIncomplete, "undefined field %q", s)
				}
				return ctx.mkErr(x, index, "undefined field %q", s)
			}
			return n.Value
		}
	case atter:
		if e.is(index, intKind, msgIndexType, k) {
			i := index.(*numLit).intValue(ctx)
			if i < 0 {
				const msg = "invalid %[4]s index %[1]s (index must be non-negative)"
				return e.mkErr(x.Index, index, 0, k, msg)
			}
			return v.at(ctx, i)
		}
	}
	return e.err(&indexExpr{x.baseValue, val, index})
}

func (x *indexExpr) reference(ctx *context) (result value) {
	if ctx.trace {
		defer uni(indent(ctx, "indexExpr", x))
		defer func() { ctx.debugPrint("result:", result) }()
	}

	e := newEval(ctx, true)

	const msgType = "invalid operation: %[5]s (type %[3]s does not support indexing)"
	const msgIndexType = "invalid %[5]s index %[1]s (type %[3]s)"

	val := e.eval(x.X, listKind|structKind, msgType, x)
	k := val.Kind()
	index := e.eval(x.Index, stringKind|intKind, msgIndexType, k)

	switch v := val.(type) {
	case *structLit:
		if e.is(index, stringKind, msgIndexType, k) {
			s := index.strValue()
			// TODO: must lookup
			n := v.Lookup(ctx, ctx.StrLabel(s))
			if n.definition {
				return ctx.mkErr(x, index,
					"field %q is a definition", s)
			}
			if n.optional {
				return ctx.mkErr(x, index, codeIncomplete, "field %q is optional", s)
			}
			if n.val() == nil {
				if !v.isClosed() {
					return ctx.mkErr(x, index, codeIncomplete, "undefined field %q", s)
				}
				return ctx.mkErr(x, index, "undefined field %q", s)
			}
			return n.v
		}
	case *list:
		if e.is(index, intKind, msgIndexType, k) {
			i := index.(*numLit).intValue(ctx)
			if i < 0 {
				const msg = "invalid %[4]s index %[1]s (index must be non-negative)"
				return e.mkErr(x.Index, index, 0, k, msg)
			}
			return v.iterAt(ctx, i).v
		}

	case atter:
		if e.is(index, intKind, msgIndexType, k) {
			i := index.(*numLit).intValue(ctx)
			if i < 0 {
				const msg = "invalid %[4]s index %[1]s (index must be non-negative)"
				return e.mkErr(x.Index, index, 0, k, msg)
			}
			return v.at(ctx, i)
		}
	}
	return e.err(&indexExpr{x.baseValue, val, index})
}

// Composit

func (x *sliceExpr) evalPartial(ctx *context) (result evaluated) {
	if ctx.trace {
		defer uni(indent(ctx, "sliceExpr", x))
		defer func() { ctx.debugPrint("result:", result) }()
	}

	e := newEval(ctx, true)
	const msgType = "cannot slice %[2]s (type %[3]s)"
	const msgInvalidIndex = "invalid slice index %[1]s (type %[3]s)"
	val := e.eval(x.X, listKind, msgType)
	lo := e.evalAllowNil(x.Lo, intKind, msgInvalidIndex)
	hi := e.evalAllowNil(x.Hi, intKind, msgInvalidIndex)
	var low, high *numLit
	if lo != nil && e.is(lo, intKind, msgInvalidIndex) {
		low = lo.(*numLit)
	}
	if hi != nil && e.is(hi, intKind, msgInvalidIndex) {
		high = hi.(*numLit)
	}
	if !e.hasErr() {
		switch x := val.(type) {
		case *list:
			return x.slice(ctx, low, high)
		case *stringLit:
			return x.slice(ctx, low, high)
		}
	}
	return e.err(&sliceExpr{x.baseValue, val, lo, hi})
}

// TODO: make a callExpr a binary expression
func (x *callExpr) evalPartial(ctx *context) (result evaluated) {
	if ctx.trace {
		defer uni(indent(ctx, "callExpr", x))
		defer func() {
			ctx.debugPrint("result:", result)
		}()
	}

	e := newEval(ctx, true)

	fn := e.eval(x.Fun, lambdaKind, "cannot call non-function %[2]s (type %[3]s)")
	args := make([]evaluated, len(x.Args))
	for i, a := range x.Args {
		args[i] = e.evalPartial(a, typeKinds, "never triggers")
	}
	if !e.hasErr() {
		// If we have a template expression, it is either already copied it as
		// result of a references, or it is a literal, in which case it is
		// trivially fully evaluated.
		return fn.(caller).call(ctx, x, args...).evalPartial(ctx)
	}
	// Construct a simplified call for reporting purposes.
	err := &callExpr{x.baseValue, fn, nil}
	for _, a := range args {
		err.Args = append(err.Args, a)
	}
	return e.err(err)
}

func (x *customValidator) evalPartial(ctx *context) (result evaluated) {
	if ctx.trace {
		defer uni(indent(ctx, "custom", x))
		defer func() { ctx.debugPrint("result:", result) }()
	}
	return x
}

func (x *bound) evalPartial(ctx *context) (result evaluated) {
	if ctx.trace {
		defer uni(indent(ctx, "bound", x))
		defer func() { ctx.debugPrint("result:", result) }()
	}
	v := x.Expr.evalPartial(ctx)
	if isBottom(v) {
		if isIncomplete(v) {
			return v
		}
		return ctx.mkErr(x, v, "error evaluating bound")
	}
	if v == x.Expr {
		return x
	}
	return newBound(ctx, x.baseValue, x.Op, x.k, v)
}

func (x *interpolation) evalPartial(ctx *context) (result evaluated) {
	if ctx.trace {
		defer uni(indent(ctx, "interpolation", x))
		defer func() { ctx.debugPrint("result:", result) }()
	}
	buf := bytes.Buffer{}
	var incomplete value
	for _, v := range x.Parts {
		switch e := ctx.manifest(v).(type) {
		case *bottom:
			return e
		case *stringLit, *numLit, *durationLit:
			buf.WriteString(e.strValue())
		default:
			k := e.Kind()
			if k&stringableKind == bottomKind {
				return ctx.mkErr(e, "expression in interpolation must evaluate to a number kind or string (found %v)", k)
			}
			if !k.isGround() {
				incomplete = v
			}
		}
	}
	if incomplete != nil {
		return ctx.mkErr(incomplete, codeIncomplete,
			"incomplete value '%s' in interpolation", ctx.str(incomplete))
	}
	return &stringLit{x.baseValue, buf.String(), nil}
}

func (x *list) evalPartial(ctx *context) (result evaluated) {
	if ctx.trace {
		defer uni(indent(ctx, "list", x))
		defer func() { ctx.debugPrint("result:", result) }()
	}
	n := x.len.evalPartial(ctx)
	if isBottom(n) {
		return n
	}
	s := x.elem.evalPartial(ctx).(*structLit)
	if s == x.elem && n == x.len {
		return x
	}
	return &list{x.baseValue, s, x.typ, n}
}

func (x *listComprehension) evalPartial(ctx *context) evaluated {
	s := &structLit{baseValue: x.baseValue}
	list := &list{baseValue: x.baseValue, elem: s}
	err := x.clauses.yield(ctx, func(v evaluated) *bottom {
		list.elem.Arcs = append(list.elem.Arcs, arc{
			Label: label(len(list.elem.Arcs)),
			v:     v.evalPartial(ctx),
		})
		return nil
	})
	if err != nil {
		return err
	}
	list.initLit()
	return list
}

func (x *structComprehension) evalPartial(ctx *context) evaluated {
	var st evaluated = &structLit{baseValue: x.baseValue}
	err := x.clauses.yield(ctx, func(v evaluated) *bottom {
		embed := v.evalPartial(ctx)
		if st, ok := embed.(*structLit); ok {
			x, err := st.expandFields(ctx)
			if err != nil {
				return err
			}
			embed = x
		}
		res := binOp(ctx, x, opUnify, st, embed)
		if b, ok := res.(*bottom); ok {
			return b
		}
		st = res
		return nil
	})
	if err != nil {
		return err
	}
	return st
}

func (x *feed) evalPartial(ctx *context) evaluated  { return x }
func (x *guard) evalPartial(ctx *context) evaluated { return x }
func (x *yield) evalPartial(ctx *context) evaluated { return x }

func (x *fieldComprehension) evalPartial(ctx *context) evaluated {
	k := x.key.evalPartial(ctx)
	v := x.val
	if err := firstBottom(k, v); err != nil {
		return err
	}
	if !k.Kind().isAnyOf(stringKind) {
		return ctx.mkErr(k, "key must be of type string")
	}
	f := ctx.Label(k.strValue(), true)
	st := &structLit{baseValue: x.baseValue}
	st.insertValue(ctx, f, x.opt, x.def, v, x.attrs, x.doc)
	return st
}

func (x *closeIfStruct) evalPartial(ctx *context) evaluated {
	v := x.value.evalPartial(ctx)
	v = updateCloseStatus(ctx, v)
	return v
}

func (x *structLit) evalPartial(ctx *context) (result evaluated) {
	if ctx.trace {
		defer uni(indent(ctx, "struct eval", x))
		defer func() { ctx.debugPrint("result:", result) }()
	}
	x = ctx.deref(x).(*structLit)

	// TODO: Handle cycle?

	// TODO: would be great to be able to expand fields here. But would need
	// some careful consideration regarding dereferencing.

	return x
}

func (x *unification) evalPartial(ctx *context) (result evaluated) {
	// By definition, all of the values in this type are already evaluated.
	return x
}

func (x *disjunction) evalPartial(ctx *context) (result evaluated) {
	if ctx.trace {
		defer uni(indent(ctx, "disjunction", x))
		defer func() { ctx.debugPrint("result:", result) }()
	}

	// decSum := false
	if len(ctx.evalStack) > 1 {
		ctx.inSum++
	}
	dn := &disjunction{
		x.baseValue,
		make([]dValue, 0, len(x.Values)),
		make([]*bottom, 0, len(x.errors)),
		x.HasDefaults,
	}
	changed := false
	for _, v := range x.Values {
		n := v.Val.evalPartial(ctx)
		changed = changed || n != v.Val
		// Including elements of disjunctions recursively makes default handling
		// associative (*a | (*b|c)) == ((*a|*b) | c).
		if d, ok := n.(*disjunction); ok {
			changed = true
			for _, dv := range d.Values {
				dn.add(ctx, dv.Val, dv.Default)
			}
		} else {
			dn.add(ctx, n, v.Default)
		}
	}
	if !changed {
		dn = x
	}
	if len(ctx.evalStack) > 1 {
		ctx.inSum--
	}
	return dn.normalize(ctx, x).val
}

func (x *disjunction) manifest(ctx *context) (result evaluated) {
	values := make([]dValue, 0, len(x.Values))
	validValue := false
	for _, dv := range x.Values {
		switch {
		case isBottom(dv.Val):
		case dv.Default:
			values = append(values, dv)
		default:
			validValue = true
		}
	}

	switch {
	case len(values) > 0:
		// values contains all the valid defaults
	case !validValue:
		return x
	default:
		for _, dv := range x.Values {
			dv.Default = false
			values = append(values, dv)
		}
	}

	switch len(values) {
	case 0:
		return x

	case 1:
		return values[0].Val.evalPartial(ctx)
	}

	x = &disjunction{x.baseValue, values, x.errors, true}
	return x.normalize(ctx, x).val
}

func (x *binaryExpr) evalPartial(ctx *context) (result evaluated) {
	if ctx.trace {
		defer uni(indent(ctx, "binaryExpr", x))
		defer func() { ctx.debugPrint("result:", result) }()
	}
	var left, right evaluated

	if _, isUnify := x.Op.unifyType(); !isUnify {
		ctx.incEvalDepth()
		left = ctx.manifest(x.X)
		right = ctx.manifest(x.Y)
		ctx.decEvalDepth()

		// TODO: allow comparing to a literal bottom only. Find something more
		// principled perhaps. One should especially take care that two values
		// evaluating to bottom don't evaluate to true. For now we check for
		// bottom here and require that one of the values be a bottom literal.
		if isLiteralBottom(x.X) || isLiteralBottom(x.Y) {
			if b := validate(ctx, left); b != nil {
				left = b
			}
			if b := validate(ctx, right); b != nil {
				right = b
			}
			leftBottom := isBottom(left)
			rightBottom := isBottom(right)
			switch x.Op {
			case opEql:
				return &boolLit{x.baseValue, leftBottom == rightBottom}
			case opNeq:
				return &boolLit{x.baseValue, leftBottom != rightBottom}
			}
		}
	} else {
		left = resolveReference(ctx, x.X)
		right = resolveReference(ctx, x.Y)

		if err := cycleError(left); err != nil && ctx.inSum == 0 && right.Kind().isAtom() {
			return ctx.delayConstraint(right, x)
		}
		if err := cycleError(right); err != nil && ctx.inSum == 0 && left.Kind().isAtom() {
			return ctx.delayConstraint(left, x)
		}

		// check if it is a cycle that can be unwrapped.
		// If other value is a cycle or list, return the original forwarded,
		// but ensure the value is not cached. Object/list error?
	}
	return binOp(ctx, x, x.Op, left, right)
}

func (x *unaryExpr) evalPartial(ctx *context) (result evaluated) {
	if ctx.trace {
		defer uni(indent(ctx, "unaryExpr", x))
		defer func() { ctx.debugPrint("result:", result) }()
	}

	return evalUnary(ctx, x, x.Op, x.X)
}

func evalUnary(ctx *context, src source, op op, x value) evaluated {
	v := ctx.manifest(x)

	const numeric = numKind | durationKind
	kind := v.Kind()
	switch op {
	case opSub:
		if kind&numeric == bottomKind {
			return ctx.mkErr(src, "invalid operation -%s (- %s)", ctx.str(x), kind)
		}
		switch v := v.(type) {
		case *numLit:
			f := *v
			f.X.Neg(&v.X)
			return &f
		case *durationLit:
			d := *v
			d.d = -d.d
			return &d
		}
		return ctx.mkErr(src, codeIncomplete, "operand %s of '-' not concrete (was %s)", ctx.str(x), kind)

	case opAdd:
		if kind&numeric == bottomKind {
			return ctx.mkErr(src, "invalid operation +%s (+ %s)", ctx.str(x), kind)
		}
		switch v := v.(type) {
		case *numLit, *durationLit:
			return v
		case *top:
			return &basicType{v.baseValue, numeric | nonGround}
		case *basicType:
			return &basicType{v.baseValue, (v.K & numeric) | nonGround}
		}
		return ctx.mkErr(src, codeIncomplete, "operand %s of '+' not concrete (was %s)", ctx.str(x), kind)

	case opNot:
		if kind&boolKind == bottomKind {
			return ctx.mkErr(src, "invalid operation !%s (! %s)", ctx.str(x), kind)
		}
		switch v := v.(type) {
		case *boolLit:
			return &boolLit{src.base(), !v.B}
		}
		return ctx.mkErr(src, codeIncomplete, "operand %s of '!' not concrete (was %s)", ctx.str(x), kind)
	}
	return ctx.mkErr(src, "invalid operation %s%s (%s %s)", op, ctx.str(x), op, kind)
}
