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
	v := e.eval(x.x, structKind|lambdaKind, msgType, x)

	if e.is(v, structKind|lambdaKind, "") {
		sc, ok := v.(scope)
		if !ok {
			return ctx.mkErr(x, "invalid subject to selector (found %v)", v.kind())
		}
		n := sc.lookup(ctx, x.feature)
		if n.optional {
			field := ctx.labelStr(x.feature)
			return ctx.mkErr(x, codeIncomplete, "field %q is optional", field)
		}
		if n.val() == nil {
			field := ctx.labelStr(x.feature)
			if st, ok := sc.(*structLit); ok && !st.isClosed() {
				return ctx.mkErr(x, codeIncomplete, "undefined field %q", field)
			}
			//	m.foo undefined (type map[string]bool has no field or method foo)
			// TODO: mention x.x in error message?
			return ctx.mkErr(x, "undefined field %q", field)
		}
		// TODO: do we need to evaluate here?
		return n.cache.evalPartial(ctx)
	}
	return e.err(&selectorExpr{x.baseValue, v, x.feature})
}

func (x *indexExpr) evalPartial(ctx *context) (result evaluated) {
	if ctx.trace {
		defer uni(indent(ctx, "indexExpr", x))
		defer func() { ctx.debugPrint("result:", result) }()
	}

	e := newEval(ctx, true)

	const msgType = "invalid operation: %[5]s (type %[3]s does not support indexing)"
	const msgIndexType = "invalid %[5]s index %[1]s (type %[3]s)"

	val := e.eval(x.x, listKind|structKind, msgType, x)
	k := val.kind()
	index := e.eval(x.index, stringKind|intKind, msgIndexType, k)

	switch v := val.(type) {
	case *structLit:
		if e.is(index, stringKind, msgIndexType, k) {
			s := index.strValue()
			// TODO: must lookup
			n := v.lookup(ctx, ctx.strLabel(s))
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
			return n.cache
		}
	case atter:
		if e.is(index, intKind, msgIndexType, k) {
			i := index.(*numLit).intValue(ctx)
			if i < 0 {
				const msg = "invalid %[4]s index %[1]s (index must be non-negative)"
				return e.mkErr(x.index, index, 0, k, msg)
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
	val := e.eval(x.x, listKind, msgType)
	lo := e.evalAllowNil(x.lo, intKind, msgInvalidIndex)
	hi := e.evalAllowNil(x.hi, intKind, msgInvalidIndex)
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

	fn := e.eval(x.x, lambdaKind, "cannot call non-function %[2]s (type %[3]s)")
	args := make([]evaluated, len(x.args))
	for i, a := range x.args {
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
		err.args = append(err.args, a)
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
	v := x.value.evalPartial(ctx)
	if isBottom(v) {
		if isIncomplete(v) {
			return v
		}
		return ctx.mkErr(x, v, "error evaluating bound")
	}
	if v == x.value {
		return x
	}
	return newBound(ctx, x.baseValue, x.op, x.k, v)
}

func (x *interpolation) evalPartial(ctx *context) (result evaluated) {
	if ctx.trace {
		defer uni(indent(ctx, "interpolation", x))
		defer func() { ctx.debugPrint("result:", result) }()
	}
	buf := bytes.Buffer{}
	for _, v := range x.parts {
		switch e := ctx.manifest(v).(type) {
		case *bottom:
			return e
		case *stringLit, *numLit, *durationLit:
			buf.WriteString(e.strValue())
		default:
			k := e.kind()
			if k&stringableKind == bottomKind {
				return ctx.mkErr(e, "expression in interpolation must evaluate to a number kind or string (found %v)", k)
			}
			if !k.isGround() {
				return ctx.mkErr(e, codeIncomplete, "incomplete")
			}
		}
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
		list.elem.arcs = append(list.elem.arcs, arc{
			feature: label(len(list.elem.arcs)),
			v:       v.evalPartial(ctx),
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
	st := &structLit{baseValue: x.baseValue}
	err := x.clauses.yield(ctx, func(v evaluated) *bottom {
		embed := v.evalPartial(ctx).(*structLit)
		embed, err := embed.expandFields(ctx)
		if err != nil {
			return err
		}
		res := binOp(ctx, x, opUnify, st, embed)
		switch u := res.(type) {
		case *bottom:
			return u
		case *structLit:
			st = u
		default:
			panic("unreachable")
		}
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
	v := x.val.evalPartial(ctx)
	if err := firstBottom(k, v); err != nil {
		return err
	}
	if !k.kind().isAnyOf(stringKind) {
		return ctx.mkErr(k, "key must be of type string")
	}
	f := ctx.label(k.strValue(), true)
	st := &structLit{baseValue: x.baseValue}
	st.insertValue(ctx, f, x.opt, x.def, v, x.attrs, x.doc)
	return st
}

func (x *closeIfStruct) evalPartial(ctx *context) evaluated {
	v := x.value.evalPartial(ctx)
	updateCloseStatus(ctx, v)
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
		make([]dValue, 0, len(x.values)),
		make([]*bottom, 0, len(x.errors)),
		x.hasDefaults,
	}
	changed := false
	for _, v := range x.values {
		n := v.val.evalPartial(ctx)
		changed = changed || n != v.val
		// Including elements of disjunctions recursively makes default handling
		// associative (*a | (*b|c)) == ((*a|*b) | c).
		if d, ok := n.(*disjunction); ok {
			changed = true
			for _, dv := range d.values {
				dn.add(ctx, dv.val, dv.marked)
			}
		} else {
			dn.add(ctx, n, v.marked)
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
	values := make([]dValue, 0, len(x.values))
	validValue := false
	for _, dv := range x.values {
		switch {
		case isBottom(dv.val):
		case dv.marked:
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
		for _, dv := range x.values {
			dv.marked = false
			values = append(values, dv)
		}
	}

	switch len(values) {
	case 0:
		return x

	case 1:
		return values[0].val.evalPartial(ctx)
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

	if _, isUnify := x.op.unifyType(); !isUnify {
		ctx.incEvalDepth()
		left = ctx.manifest(x.left)
		right = ctx.manifest(x.right)
		ctx.decEvalDepth()

		// TODO: allow comparing to a literal bottom only. Find something more
		// principled perhaps. One should especially take care that two values
		// evaluating to bottom don't evaluate to true. For now we check for
		// bottom here and require that one of the values be a bottom literal.
		if isLiteralBottom(x.left) || isLiteralBottom(x.right) {
			leftBottom := isBottom(left)
			rightBottom := isBottom(right)
			switch x.op {
			case opEql:
				return &boolLit{x.baseValue, leftBottom == rightBottom}
			case opNeq:
				return &boolLit{x.baseValue, leftBottom != rightBottom}
			}
		}
	} else {
		left = x.left.evalPartial(ctx)
		right = x.right.evalPartial(ctx)

		if err := cycleError(left); err != nil && ctx.inSum == 0 && right.kind().isAtom() {
			return ctx.delayConstraint(right, x)
		}
		if err := cycleError(right); err != nil && ctx.inSum == 0 && left.kind().isAtom() {
			return ctx.delayConstraint(left, x)
		}

		// check if it is a cycle that can be unwrapped.
		// If other value is a cycle or list, return the original forwarded,
		// but ensure the value is not cached. Object/list error?
	}
	return binOp(ctx, x, x.op, left, right)
}

func (x *unaryExpr) evalPartial(ctx *context) (result evaluated) {
	if ctx.trace {
		defer uni(indent(ctx, "unaryExpr", x))
		defer func() { ctx.debugPrint("result:", result) }()
	}

	return evalUnary(ctx, x, x.op, x.x)
}

func evalUnary(ctx *context, src source, op op, x value) evaluated {
	v := ctx.manifest(x)

	const numeric = numKind | durationKind
	kind := v.kind()
	switch op {
	case opSub:
		if kind&numeric == bottomKind {
			return ctx.mkErr(src, "invalid operation -%s (- %s)", ctx.str(x), kind)
		}
		switch v := v.(type) {
		case *numLit:
			f := *v
			f.v.Neg(&v.v)
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
			return &basicType{v.baseValue, (v.k & numeric) | nonGround}
		}
		return ctx.mkErr(src, codeIncomplete, "operand %s of '+' not concrete (was %s)", ctx.str(x), kind)

	case opNot:
		if kind&boolKind == bottomKind {
			return ctx.mkErr(src, "invalid operation !%s (! %s)", ctx.str(x), kind)
		}
		switch v := v.(type) {
		case *boolLit:
			return &boolLit{src.base(), !v.b}
		}
		return ctx.mkErr(src, codeIncomplete, "operand %s of '!' not concrete (was %s)", ctx.str(x), kind)
	}
	return ctx.mkErr(src, "invalid operation %s%s (%s %s)", op, ctx.str(x), op, kind)
}
