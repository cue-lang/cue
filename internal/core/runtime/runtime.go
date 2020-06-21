// Copyright 2020 CUE Authors
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

package runtime

import (
	"reflect"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal/core/adt"
)

// A Runtime maintains data structures for indexing and resuse for evaluation.
type Runtime struct {
	index *Index
}

// New creates a new Runtime.
func New() *Runtime {
	return &Runtime{
		index: NewIndex(SharedIndexNew),
	}
}

func (x *Runtime) IndexToString(i int64) string {
	return x.index.IndexToString(i)
}

func (x *Runtime) StringToIndex(s string) int64 {
	return x.index.StringToIndex(s)
}

func (x *Runtime) LoadImport(importPath string) (*adt.Vertex, errors.Error) {
	v := x.index.GetImportFromPath(importPath)
	if v == nil {
		return nil, nil
	}
	return v.(*adt.Vertex), nil
}

func (x *Runtime) StoreType(t reflect.Type, src ast.Expr, expr adt.Expr) {
	if expr == nil {
		x.index.StoreType(t, src)
	} else {
		x.index.StoreType(t, expr)
	}
}

func (x *Runtime) LoadType(t reflect.Type) (src ast.Expr, expr adt.Expr, ok bool) {
	v, ok := x.index.LoadType(t)
	if ok {
		switch x := v.(type) {
		case ast.Expr:
			return x, nil, true
		case adt.Expr:
			src, _ = x.Source().(ast.Expr)
			return src, x, true
		}
	}
	return nil, nil, false
}
