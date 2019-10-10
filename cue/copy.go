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

func (c *context) copy(v value) value {
	// return v.copy(c)
	return rewrite(c, v, rewriteCopy)
}

func rewriteCopy(ctx *context, v value) (value, bool) {
	switch x := v.(type) {
	case *nodeRef:
		node := ctx.deref(x.node)
		if node == x.node {
			return x, false
		}
		return &nodeRef{x.baseValue, node, x.label}, false

	case *structLit:
		arcs := make(arcs, len(x.arcs))

		obj := &structLit{x.baseValue, nil, nil, x.closeStatus, nil, arcs, nil}

		defer ctx.pushForwards(x, obj).popForwards()

		emit := x.emit
		if emit != nil {
			emit = ctx.copy(x.emit)
		}
		obj.emit = emit

		t := x.template
		if t != nil {
			v := ctx.copy(t)
			if isBottom(v) {
				return t, false
			}
			t = v
		}
		obj.template = t

		for i, a := range x.arcs {
			a.setValue(ctx.copy(a.v))
			arcs[i] = a
		}

		comp := make([]value, len(x.comprehensions))
		for i, c := range x.comprehensions {
			comp[i] = ctx.copy(c)
		}
		obj.comprehensions = comp
		return obj, false

	case *lambdaExpr:
		arcs := make([]arc, len(x.arcs))
		for i, a := range x.arcs {
			arcs[i] = arc{feature: a.feature, v: ctx.copy(a.v)}
		}
		lambda := &lambdaExpr{x.baseValue, &params{arcs}, nil}
		defer ctx.pushForwards(x, lambda).popForwards()

		lambda.value = ctx.copy(x.value)
		return lambda, false
	}
	return v, true
}
