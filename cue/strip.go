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

// This file defines a rewriter that strips a fully evaluated value of its
// template.

// TODO: currently strip templates does a full evaluation as it is hard to keep
// nodeRef and copied structs in sync. This is far from efficient, but it is the
// easiest to get correct.

// stripTemplates evaluates v and strips the result of templates.
func stripTemplates(ctx *context, v value) value {
	return rewrite(ctx, v, stripRewriter)
}

func stripRewriter(ctx *context, v value) (value, bool) {
	eval := ctx.manifest(v)
	switch x := eval.(type) {
	case *structLit:
		x = x.expandFields(ctx)
		if x.template != nil {
			arcs := make(arcs, len(x.arcs))
			for i, a := range x.arcs {
				a.setValue(rewrite(ctx, x.at(ctx, i), stripRewriter))
				arcs[i] = a
			}
			// TODO: verify that len(x.comprehensions) == 0
			return &structLit{x.baseValue, x.emit, nil, nil, arcs, nil}, false
		}
	}
	return eval, true
}
