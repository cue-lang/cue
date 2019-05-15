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
	"sort"
)

// A mergedValues type merges structs without unifying their templates.
// It evaluates structs in parallel and then creates a new mergedValues
// for each duplicate arc. The mergedValues do not reappear once there is
// only a single value per arc.
//
// This is used to merge different instances which may have incompatible
// specializations, but have disjuncts objects that may otherwise be shared
// in the same namespace.
type mergedValues struct {
	baseValue
	values []value
}

func (x *mergedValues) evalPartial(ctx *context) evaluated {
	var structs []*structLit
	for _, v := range x.values {
		v = v.evalPartial(ctx)
		o, ok := v.(*structLit)
		if !ok {
			v := x.values[0]
			for _, w := range x.values[1:] {
				v = mkBin(ctx, w.Pos(), opUnify, v, w)
			}
			return v.evalPartial(ctx)
		}
		o = o.expandFields(ctx)
		structs = append(structs, o)
	}

	// Pre-expand the arcs so that we can discard the templates.
	obj := &structLit{
		baseValue: structs[0].baseValue,
	}
	var arcs arcs
	for _, v := range structs {
		for i := 0; i < len(v.arcs); i++ {
			w := v.iterAt(ctx, i)
			arcs = append(arcs, w)
		}
	}
	obj.arcs = arcs
	sort.Stable(obj)

	values := []value{}
	for _, v := range structs {
		if v.emit != nil {
			values = append(values, v.emit)
		}
	}
	switch len(values) {
	case 0:
	case 1:
		obj.emit = values[0]
	default:
		obj.emit = &mergedValues{values[0].base(), values}
	}

	// merge arcs
	k := 0
	for i := 0; i < len(arcs); k++ {
		a := arcs[i]
		// TODO: consider storing the evaluated value. This is a performance
		// versus having more information tradeoff. It results in the same
		// value.
		values := []value{a.v}
		for i++; i < len(arcs) && a.feature == arcs[i].feature; i++ {
			values = append(values, arcs[i].v)
			a.optional = a.optional && arcs[i].optional
			var err evaluated
			a.attrs, err = unifyAttrs(ctx, a.v, a.attrs, arcs[i].attrs)
			if err != nil {
				return err
			}
			a.docs = mergeDocs(a.docs, arcs[i].docs)
		}
		if len(values) == 1 {
			arcs[k] = a
			continue
		}
		a.cache = nil
		a.v = &mergedValues{a.v.base(), values}
		arcs[k] = a
	}
	obj.arcs = arcs[:k]
	return obj
}

func (x *mergedValues) kind() kind {
	k := x.values[0].kind()
	for _, v := range x.values {
		k = unifyType(k, v.kind())
	}
	return k
}

func (x *mergedValues) rewrite(ctx *context, fn rewriteFunc) value {
	vs := make([]value, len(x.values))
	for i, v := range x.values {
		vs[i] = rewrite(ctx, v, fn)
	}
	return &mergedValues{x.baseValue, vs}
}

func (x *mergedValues) subsumesImpl(ctx *context, v value, mode subsumeMode) bool {
	return false
}
