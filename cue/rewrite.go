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

// TODO: nodeRefs are currently not updated if the structs they point to are
// updated. Handing this in uses of rewrite is tedious and hard to get correct.
// Make this a general mechanism. This can be done using a Tomabechi-like
// approach of associating copies with nodes in one pass, and then make a
// complete copy in a second.

type rewriteFunc func(ctx *context, v value) (value, bool)

func rewrite(ctx *context, v value, fn rewriteFunc) value {
	v, descend := fn(ctx, v)
	if !descend {
		return v
	}
	return v.rewrite(ctx, fn)
}

func (x *nodeRef) rewrite(ctx *context, fn rewriteFunc) value {
	return x
}

func (x *closeIfStruct) rewrite(ctx *context, fn rewriteFunc) value {
	v := rewrite(ctx, x.value, fn)
	if v == x.value {
		return x
	}
	return wrapFinalize(ctx, v)
}

func (x *structLit) rewrite(ctx *context, fn rewriteFunc) value {
	emit := x.emit
	if emit != nil {
		emit = rewrite(ctx, x.emit, fn)
	}
	arcs := make(arcs, len(x.arcs))
	obj := &structLit{baseValue: x.baseValue, emit: emit, arcs: arcs}
	changed := emit == x.emit
	for i, a := range x.arcs {
		a.setValue(rewrite(ctx, a.v, fn))
		changed = changed || arcs[i].v != a.v
		arcs[i] = a
	}
	if !changed {
		return x
	}
	return obj
}

func (x *selectorExpr) rewrite(ctx *context, fn rewriteFunc) value {
	v := rewrite(ctx, x.X, fn)
	if v == x.X {
		return x
	}
	return &selectorExpr{x.baseValue, v, x.Sel}
}

func (x *indexExpr) rewrite(ctx *context, fn rewriteFunc) value {
	v := rewrite(ctx, x.X, fn)
	index := rewrite(ctx, x.Index, fn)
	if v == x.X && index == x.Index {
		return x
	}
	return &indexExpr{x.baseValue, v, index}
}

// Even more boring stuff below.

func (x *builtin) rewrite(ctx *context, fn rewriteFunc) value     { return x }
func (x *top) rewrite(ctx *context, fn rewriteFunc) value         { return x }
func (x *bottom) rewrite(ctx *context, fn rewriteFunc) value      { return x }
func (x *basicType) rewrite(ctx *context, fn rewriteFunc) value   { return x }
func (x *nullLit) rewrite(ctx *context, fn rewriteFunc) value     { return x }
func (x *boolLit) rewrite(ctx *context, fn rewriteFunc) value     { return x }
func (x *stringLit) rewrite(ctx *context, fn rewriteFunc) value   { return x }
func (x *bytesLit) rewrite(ctx *context, fn rewriteFunc) value    { return x }
func (x *numLit) rewrite(ctx *context, fn rewriteFunc) value      { return x }
func (x *durationLit) rewrite(ctx *context, fn rewriteFunc) value { return x }

func (x *customValidator) rewrite(ctx *context, fn rewriteFunc) value {
	args := make([]evaluated, len(x.Args))
	changed := false
	for i, a := range x.Args {
		v := rewrite(ctx, a, fn)
		args[i] = v.(evaluated)
		changed = changed || v != a
	}
	if !changed {
		return x
	}
	return &customValidator{baseValue: x.baseValue, Args: args, Builtin: x.Builtin}
}

func (x *bound) rewrite(ctx *context, fn rewriteFunc) value {
	v := rewrite(ctx, x.Expr, fn)
	if v == x.Expr {
		return x
	}
	return newBound(ctx, x.baseValue, x.Op, x.k, v)
}

func (x *interpolation) rewrite(ctx *context, fn rewriteFunc) value {
	parts := make([]value, len(x.Parts))
	changed := false
	for i, p := range x.Parts {
		parts[i] = rewrite(ctx, p, fn)
		changed = changed || parts[i] != p
	}
	if !changed {
		return x
	}
	return &interpolation{x.baseValue, x.K, parts}
}

func (x *list) rewrite(ctx *context, fn rewriteFunc) value {
	elem := rewrite(ctx, x.elem, fn).(*structLit)
	typ := rewrite(ctx, x.typ, fn)
	len := rewrite(ctx, x.len, fn)
	if elem == x.elem && typ == x.typ && len == x.len {
		return x
	}
	return &list{x.baseValue, elem, typ, len}
}

func (x *sliceExpr) rewrite(ctx *context, fn rewriteFunc) value {
	v := rewrite(ctx, x.X, fn)
	var lo, hi value
	if x.Lo != nil {
		lo = rewrite(ctx, x.Lo, fn)
	}
	if x.Hi != nil {
		hi = rewrite(ctx, x.Hi, fn)
	}
	if v == x.X && lo == x.Lo && hi == x.Hi {
		return x
	}
	return &sliceExpr{x.baseValue, v, lo, hi}
}

func (x *callExpr) rewrite(ctx *context, fn rewriteFunc) value {
	args := make([]value, len(x.Args))
	changed := false
	for i, a := range x.Args {
		v := rewrite(ctx, a, fn)
		args[i] = v
		changed = changed || v != a
	}
	v := rewrite(ctx, x.Fun, fn)
	if !changed && v == x.Fun {
		return x
	}
	return &callExpr{baseValue: x.baseValue, Fun: v, Args: args}
}

func (x *lambdaExpr) rewrite(ctx *context, fn rewriteFunc) value {
	arcs := make([]arc, len(x.arcs))
	changed := false
	for i, a := range x.arcs {
		v := rewrite(ctx, a.v, fn)
		arcs[i] = arc{feature: a.feature, v: v}
		changed = changed || v != a.v
	}
	value := rewrite(ctx, x.value, fn)
	if !changed && value == x.value {
		return x
	}
	return &lambdaExpr{x.baseValue, &params{arcs}, value}
}

func (x *unaryExpr) rewrite(ctx *context, fn rewriteFunc) value {
	v := rewrite(ctx, x.X, fn)
	if v == x.X {
		return x
	}
	return &unaryExpr{x.baseValue, x.Op, v}
}

func (x *binaryExpr) rewrite(ctx *context, fn rewriteFunc) value {
	left := rewrite(ctx, x.X, fn)
	right := rewrite(ctx, x.Y, fn)
	if left == x.X && right == x.Y {
		return x
	}
	return updateBin(ctx, &binaryExpr{x.baseValue, x.Op, left, right})
}

func (x *unification) rewrite(ctx *context, fn rewriteFunc) value {
	values := make([]evaluated, len(x.Values))
	changed := false
	for i, v := range x.Values {
		values[i] = rewrite(ctx, v, fn).(evaluated)
		changed = changed || v != values[i]
	}
	if !changed {
		return x
	}
	return &unification{x.baseValue, values}
}

func (x *disjunction) rewrite(ctx *context, fn rewriteFunc) value {
	values := make([]dValue, len(x.Values))
	changed := false
	for i, d := range x.Values {
		v := rewrite(ctx, d.Val, fn)
		values[i] = dValue{v, d.Default}
		changed = changed || v != d.Val
	}
	if !changed {
		return x
	}
	return &disjunction{x.baseValue, values, x.errors, x.HasDefaults}
}

func (x *listComprehension) rewrite(ctx *context, fn rewriteFunc) value {
	clauses := rewrite(ctx, x.clauses, fn).(yielder)
	if clauses == x.clauses {
		return x
	}
	return &listComprehension{x.baseValue, clauses}
}

func (x *structComprehension) rewrite(ctx *context, fn rewriteFunc) value {
	clauses := rewrite(ctx, x.clauses, fn).(yielder)
	if clauses == x.clauses {
		return x
	}
	return &structComprehension{x.baseValue, clauses}
}

func (x *fieldComprehension) rewrite(ctx *context, fn rewriteFunc) value {
	key := rewrite(ctx, x.key, fn)
	val := rewrite(ctx, x.val, fn)
	if key == x.key && val == x.val {
		return x
	}
	return &fieldComprehension{x.baseValue, key, val, x.opt, x.def, x.doc, x.attrs}
}

func (x *yield) rewrite(ctx *context, fn rewriteFunc) value {
	value := rewrite(ctx, x.value, fn)
	if value == x.value {
		return x
	}
	return &yield{x.baseValue, value}
}

func (x *guard) rewrite(ctx *context, fn rewriteFunc) value {
	condition := rewrite(ctx, x.Condition, fn)
	value := rewrite(ctx, x.Dst, fn).(yielder)
	if condition == x.Condition && value == x.Dst {
		return x
	}
	return &guard{x.baseValue, condition, value}
}

func (x *feed) rewrite(ctx *context, fn rewriteFunc) value {
	source := rewrite(ctx, x.Src, fn)
	lambda := rewrite(ctx, x.fn, fn).(*lambdaExpr)
	if source == x.Src && lambda == x.fn {
		return x
	}
	return &feed{x.baseValue, source, lambda}
}
