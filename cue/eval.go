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
		n, _ := v.(scope).lookup(ctx, x.feature)
		if n == nil {
			field := ctx.labelStr(x.feature)
			//	m.foo undefined (type map[string]bool has no field or method foo)
			return ctx.mkErr(x, "undefined field %q", field)
		}
		return n.evalPartial(ctx)
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

	val := e.eval(x.x, listKind|structKind|stringKind|bytesKind, msgType, x)
	k := val.kind()
	index := e.eval(x.index, stringKind|intKind, msgIndexType, k)

	switch v := val.(type) {
	case *structLit:
		if e.is(index, stringKind, msgIndexType, k) {
			s := index.strValue()
			// TODO: must lookup
			n, _ := v.lookup(ctx, ctx.strLabel(s))
			if n == nil {
				return ctx.mkErr(x, index, "undefined field %q", s)
			}
			return n
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
	val := e.eval(x.x, listKind|stringKind, msgType)
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

	fn := e.eval(x.x, lambdaKind, "cannot call non-function %[1]s (type %[3]s)")
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

func (x *rangeLit) evalPartial(ctx *context) (result evaluated) {
	if ctx.trace {
		defer uni(indent(ctx, "rangeLit", x))
		defer func() { ctx.debugPrint("result:", result) }()
	}
	rngFrom := x.from.evalPartial(ctx)
	rngTo := x.to.evalPartial(ctx)
	// rngFrom := ctx.manifest(x.from)
	// rngTo := ctx.manifest(x.to)
	// kind := unifyType(rngFrom.kind(), rngTo.kind())
	// TODO: sufficient to do just this?
	kind, _ := matchBinOpKind(opLeq, rngFrom.kind(), rngTo.kind())
	if kind&comparableKind == bottomKind {
		return ctx.mkErr(x, "invalid range: must be defined for strings or numbers")
	}
	// Collapse evaluated nested ranges
	if from, ok := rngFrom.(*rangeLit); ok {
		rngFrom = from.from.(evaluated)
	}
	if to, ok := rngTo.(*rangeLit); ok {
		rngTo = to.to.(evaluated)
	}
	rng := &rangeLit{x.baseValue, rngFrom, rngTo}
	if !rngFrom.kind().isGround() || !rngTo.kind().isGround() {
		return rng
	}
	// validate range
	comp := binOp(ctx, x, opLeq, rngFrom, rngTo)
	if isBottom(comp) {
		return ctx.mkErr(comp, "invalid range")
	}
	if !comp.(*boolLit).b {
		return ctx.mkErr(x, "for ranges from <= to, found %v > %v", rngFrom, rngTo)
	}
	if binOp(ctx, x, opEql, rngFrom, rngTo).(*boolLit).b {
		return rngFrom
	}
	return rng
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
	return &stringLit{x.baseValue, buf.String()}
}

func (x *list) evalPartial(ctx *context) (result evaluated) {
	if ctx.trace {
		defer uni(indent(ctx, "list", x))
		defer func() { ctx.debugPrint("result:", result) }()
	}
	n := x.len.evalPartial(ctx)
	t := x.typ.evalPartial(ctx)
	if err := firstBottom(n, t); err != nil {
		return err
	}
	a := make([]value, len(x.a))
	changed := false
	for i, v := range x.a {
		// TODO: don't evaluate now. List elements may refer to other list
		// elements. Evaluating them here will cause a cycle evaluating the
		// struct field.
		e := v.evalPartial(ctx)
		changed = changed || e != v
		switch e.(type) {
		case *bottom:
			return e
		case value:
			a[i] = e
		}
	}
	if !changed && n == x.len && t == x.typ {
		return x
	}
	return &list{x.baseValue, a, t, n}
}

func (x *listComprehension) evalPartial(ctx *context) evaluated {
	list := &list{baseValue: x.baseValue}
	result := x.clauses.yield(ctx, func(k, v evaluated) *bottom {
		if !k.kind().isAnyOf(intKind) {
			return ctx.mkErr(k, "key must be of type int")
		}
		list.a = append(list.a, v.evalPartial(ctx))
		return nil
	})
	switch {
	case result == nil:
	case isBottom(result):
		return result
	default:
		panic("should not happen")
	}
	list.initLit()
	return list
}

func (x *feed) evalPartial(ctx *context) evaluated  { return x }
func (x *guard) evalPartial(ctx *context) evaluated { return x }
func (x *yield) evalPartial(ctx *context) evaluated { return x }

func (x *fieldComprehension) evalPartial(ctx *context) evaluated { return x }

func (x *structLit) evalPartial(ctx *context) (result evaluated) {
	if ctx.trace {
		defer uni(indent(ctx, "struct eval", x))
		defer func() { ctx.debugPrint("result:", result) }()
	}
	x = ctx.deref(x).(*structLit)

	// TODO: Handle cycle?

	return x
}

func (x *disjunction) evalPartial(ctx *context) (result evaluated) {
	if ctx.trace {
		defer uni(indent(ctx, "disjunction", x))
		defer func() { ctx.debugPrint("result:", result) }()
	}

	dn := &disjunction{x.baseValue, make([]dValue, 0, len(x.values))}
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
	return dn.normalize(ctx, x).val
}

func (x *disjunction) manifest(ctx *context) (result evaluated) {
	var err, marked, unmarked1, unmarked2 evaluated
	for _, d := range x.values {
		// Because of the lazy evaluation strategy, we may still have
		// latent unification.
		if err := validate(ctx, d.val); err != nil {
			continue
		}
		switch {
		case d.marked:
			if marked != nil {
				// TODO: allow disjunctions to be returned as is.
				return ctx.mkErr(x, "more than one default remaining (%v and %v)", debugStr(ctx, marked), debugStr(ctx, d.val))
			}
			marked = d.val.(evaluated)
		case unmarked1 == nil:
			unmarked1 = d.val.(evaluated)
		default:
			unmarked2 = d.val.(evaluated)
		}
	}
	switch {
	case marked != nil:
		return marked

	case unmarked2 != nil:
		return ctx.mkErr(x, "more than one element remaining (%v and %v)",
			debugStr(ctx, unmarked1), debugStr(ctx, unmarked2))

	case unmarked1 != nil:
		return unmarked1

	case err != nil:
		return err

	default:
		return ctx.mkErr(x, "empty disjunction")
	}
}

func (x *binaryExpr) evalPartial(ctx *context) (result evaluated) {
	if ctx.trace {
		defer uni(indent(ctx, "binaryExpr", x))
		defer func() { ctx.debugPrint("result:", result) }()
	}
	var left, right evaluated

	if x.op != opUnify {
		left = ctx.manifest(x.left)
		right = ctx.manifest(x.right)

		// TODO: allow comparing to a literal bottom only. Find something more
		// principled perhaps. One should especially take care that two values
		// evaluating to bottom don't evaluate to true. For now we check for
		// bottom here and require that one of the values be a bottom literal.
		if l, r := isBottom(x.left), isBottom(x.right); l || r {
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

		if err := cycleError(left); err != nil && right.kind().isAtom() {
			return ctx.delayConstraint(right,
				mkBin(ctx, x.Pos(), opUnify, x.left, right))
		}
		if err := cycleError(right); err != nil && left.kind().isAtom() {
			return ctx.delayConstraint(left,
				mkBin(ctx, x.Pos(), opUnify, left, x.right))
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
			return ctx.mkErr(src, "unary '-' requires numeric value, found %s", kind)
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
		fallthrough

	case opAdd:
		if kind&numeric == bottomKind {
			return ctx.mkErr(src, "unary '+' requires numeric value, found %s", kind)
		}
		if kind&^(numeric|nonGround|referenceKind) == bottomKind {
			return v
		}
		switch v := v.(type) {
		case *numLit, *durationLit:
			return v
		case *top:
			return &basicType{v.baseValue, numeric | nonGround}
		case *basicType:
			return &basicType{v.baseValue, (v.k & numeric) | nonGround}
		case *rangeLit:
			from := evalUnary(ctx, src, op, v.from)
			to := evalUnary(ctx, src, op, v.to)
			return &rangeLit{src.base(), from, to}
		}

	case opNot:
		if kind&boolKind == bottomKind {
			return ctx.mkErr(src, "unary '!' requires bool value, found %s", kind)
		}
		switch v := v.(type) {
		case *top:
			return &basicType{v.baseValue, boolKind | nonGround}
		case *basicType:
			return v
		case *boolLit:
			return &boolLit{src.base(), !v.b}
		}
	}
	return ctx.mkErr(src, "invalid operand type %v for unary operator %v", v, op)
}
