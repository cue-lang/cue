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
	"strings"

	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"
)

func pos(n adt.Node) (p token.Pos) {
	if n == nil {
		return
	}
	src := n.Source()
	if src == nil {
		return
	}
	return src.Pos()
}

func init() {
	// TODO: unroll this function. Should no longer be necessary to be internal.
	internal.UnifyBuiltin = func(val interface{}, kind string) interface{} {
		v := val.(Value)
		ctx := v.ctx()

		p := strings.Split(kind, ".")
		pkg, name := p[0], p[1]
		s, _ := runtime.SharedRuntime.LoadImport(pkg)
		if s == nil {
			return v
		}
		a := s.Lookup(ctx.Label(name, false))
		if a == nil {
			return v
		}

		return v.Unify(makeValue(v.idx, a))
	}
}
