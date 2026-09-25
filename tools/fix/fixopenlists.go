// Copyright 2026 CUE Authors
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

package fix

import (
	"path"
	"strconv"
	"sync"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"
)

// fixOpenLists rewrites a file for the openlists experiment so that it
// keeps its meaning; see [rewriteOpenLists].
func fixOpenLists(f *ast.File) (*ast.File, bool) {
	imports := importPaths(f)
	changed := rewriteOpenLists(f, func(call *ast.CallExpr) bool {
		return returnsList(call, imports)
	})
	return f, changed
}

// importPaths maps the names under which a file refers to its imports to the
// import paths.
func importPaths(f *ast.File) map[string]string {
	m := map[string]string{}
	for spec := range f.ImportSpecs() {
		p, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		name := path.Base(p)
		if spec.Name != nil {
			name = spec.Name.Name
		}
		m[name] = p
	}
	return m
}

// builtinRuntime loads the builtin packages, whose declared result kinds
// tell which builtins return a list.
var builtinRuntime = sync.OnceValue(runtime.New)

// returnsList reports whether call is a call to a builtin whose declared
// result is a list, such as list.Concat. Calls it cannot resolve, including
// calls to user-defined functions, report false.
func returnsList(call *ast.CallExpr, imports map[string]string) bool {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	x, ok := sel.X.(*ast.Ident)
	if !ok {
		return false
	}
	r := builtinRuntime()
	v := r.LoadBuiltin(imports[x.Name])
	if v == nil {
		return false
	}
	name, _, _ := ast.LabelName(sel.Sel)
	arc := v.Lookup(r.Label(name, true))
	if arc == nil {
		return false
	}
	b, ok := arc.BaseValue.(*adt.Builtin)
	return ok && b.Result == adt.ListKind
}
