// Copyright 2021 CUE Authors
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

// Package value contains functions for converting values to internal types
// and various other Value-related utilities.
package value

import (
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/convert"
	"cuelang.org/go/internal/core/runtime"
)

// Context returns the cue.Context of the given argument.
//
//lint:ignore SA1019 callers may still pass the deprecated *cue.Runtime
func Context[Ctx *cue.Runtime | *cue.Context | cue.Value | *adt.OpContext](ctx Ctx) *cue.Context {
	switch x := any(ctx).(type) {
	case *cue.Runtime: //lint:ignore SA1019 see above
		(*runtime.Runtime)(x).Init()
		return (*cue.Context)(x)
	case *cue.Context:
		return x
	case cue.Value:
		r, _ := ToInternal(x)
		return (*cue.Context)(r)
	case *adt.OpContext:
		r := x.Runtime.(*runtime.Runtime)
		return (*cue.Context)(r)
	}
	panic("unreachable")
}

// OpContext returns an OpContext for the runtime and vertex underlying v.
func OpContext(v cue.Value) *adt.OpContext {
	r, x := ToInternal(v)
	return adt.NewContext(r, x)
}

func ToInternal(v cue.Value) (*runtime.Runtime, *adt.Vertex) {
	t := v.Core()
	return t.R, t.V
}

func Vertex(v cue.Value) *adt.Vertex {
	t := v.Core()
	return t.V
}

// Make wraps cue.MakeValue.
func Make(ctx *adt.OpContext, v adt.Value) cue.Value {
	return Context(ctx).Encode(v)
}

// UnifyBuiltin returns the given Value unified with the given builtin template.
func UnifyBuiltin(v cue.Value, kind string) cue.Value {
	pkg, name, _ := strings.Cut(kind, ".")
	ctx := Context(v)
	rt := (*runtime.Runtime)(ctx)
	s := rt.LoadBuiltin(pkg)
	if s == nil {
		return v
	}

	a := s.Lookup(rt.Label(name, false))
	if a == nil {
		return v
	}

	return v.Unify(ctx.Encode(a))
}

func FromGoValue(r *cue.Context, x interface{}, nilIsTop bool) cue.Value {
	rt := (*runtime.Runtime)(r)
	rt.Init()
	ctx := adt.NewContext(rt, nil)
	v := convert.FromGoValue(ctx, x, nilIsTop)
	n := adt.ToVertex(v)
	return r.Encode(n)
}

func FromGoType(r *cue.Context, x interface{}) cue.Value {
	rt := (*runtime.Runtime)(r)
	rt.Init()
	ctx := adt.NewContext(rt, nil)
	v, err := convert.FromGoType(ctx, x)
	if err != nil {
		return r.Encode(&adt.Bottom{Err: err})
	}
	return r.Encode(v)
}
