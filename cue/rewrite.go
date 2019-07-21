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
	v := rewrite(ctx, x.x, fn)
	if v == x.x {
		return x
	}
	return &selectorExpr{x.baseValue, v, x.feature}
}

func (x *indexExpr) rewrite(ctx *context, fn rewriteFunc) value {
	v := rewrite(ctx, x.x, fn)
	index := rewrite(ctx, x.index, fn)
	if v == x.x && index == x.index {
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
	args := make([]evaluated, len(x.args))
	changed := false
	for i, a := range x.args {
		v := rewrite(ctx, a, fn)
		args[i] = v.(evaluated)
		changed = changed || v != a
	}
	if !changed {
		return x
	}
	return &customValidator{baseValue: x.baseValue, args: args, call: x.call}
}

func (x *bound) rewrite(ctx *context, fn rewriteFunc) value {
	v := rewrite(ctx, x.value, fn)
	if v == x.value {
		return x
	}
	return newBound(ctx, x.baseValue, x.op, x.k, v)
}

func (x *interpolation) rewrite(ctx *context, fn rewriteFunc) value {
	parts := make([]value, len(x.parts))
	changed := false
	for i, p := range x.parts {
		parts[i] = rewrite(ctx, p, fn)
		changed = changed || parts[i] != p
	}
	if !changed {
		return x
	}
	return &interpolation{x.baseValue, x.k, parts}
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
	v := rewrite(ctx, x.x, fn)
	lo := rewrite(ctx, x.lo, fn)
	hi := rewrite(ctx, x.hi, fn)
	if v == x.x && lo == x.lo && hi == x.hi {
		return x
	}
	return &sliceExpr{x.baseValue, v, lo, hi}
}

func (x *callExpr) rewrite(ctx *context, fn rewriteFunc) value {
	args := make([]value, len(x.args))
	changed := false
	for i, a := range x.args {
		v := rewrite(ctx, a, fn)
		args[i] = v
		changed = changed || v != a
	}
	v := rewrite(ctx, x.x, fn)
	if !changed && v == x.x {
		return x
	}
	return &callExpr{baseValue: x.baseValue, x: v, args: args}
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
	v := rewrite(ctx, x.x, fn)
	if v == x.x {
		return x
	}
	return &unaryExpr{x.baseValue, x.op, v}
}

func (x *binaryExpr) rewrite(ctx *context, fn rewriteFunc) value {
	left := rewrite(ctx, x.left, fn)
	right := rewrite(ctx, x.right, fn)
	if left == x.left && right == x.right {
		return x
	}
	return updateBin(ctx, &binaryExpr{x.baseValue, x.op, left, right})
}

func (x *unification) rewrite(ctx *context, fn rewriteFunc) value {
	values := make([]evaluated, len(x.values))
	changed := false
	for i, v := range x.values {
		values[i] = rewrite(ctx, v, fn).(evaluated)
		changed = changed || v != values[i]
	}
	if !changed {
		return x
	}
	return &unification{x.baseValue, values}
}

func (x *disjunction) rewrite(ctx *context, fn rewriteFunc) value {
	values := make([]dValue, len(x.values))
	changed := false
	for i, d := range x.values {
		v := rewrite(ctx, d.val, fn)
		values[i] = dValue{v, d.marked}
		changed = changed || v != d.val
	}
	if !changed {
		return x
	}
	return &disjunction{x.baseValue, values, x.hasDefaults}
}

func (x *listComprehension) rewrite(ctx *context, fn rewriteFunc) value {
	clauses := rewrite(ctx, x.clauses, fn).(yielder)
	if clauses == x.clauses {
		return x
	}
	return &listComprehension{x.baseValue, clauses}
}

func (x *fieldComprehension) rewrite(ctx *context, fn rewriteFunc) value {
	clauses := rewrite(ctx, x.clauses, fn).(yielder)
	if clauses == x.clauses {
		return x
	}
	return &fieldComprehension{x.baseValue, clauses, x.isTemplate}
}

func (x *yield) rewrite(ctx *context, fn rewriteFunc) value {
	key := x.key
	if key != nil {
		key = rewrite(ctx, x.key, fn)
	}
	value := rewrite(ctx, x.value, fn)
	if key == x.key && value == x.value {
		return x
	}
	return &yield{x.baseValue, x.opt, x.def, key, value}
}

func (x *guard) rewrite(ctx *context, fn rewriteFunc) value {
	condition := rewrite(ctx, x.condition, fn)
	value := rewrite(ctx, x.value, fn).(yielder)
	if condition == x.condition && value == x.value {
		return x
	}
	return &guard{x.baseValue, condition, value}
}

func (x *feed) rewrite(ctx *context, fn rewriteFunc) value {
	source := rewrite(ctx, x.source, fn)
	lambda := rewrite(ctx, x.fn, fn).(*lambdaExpr)
	if source == x.source && lambda == x.fn {
		return x
	}
	return &feed{x.baseValue, source, lambda}
}
