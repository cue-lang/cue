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
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
)

// A Runtime maintains data structures for indexing and resuse for evaluation.
type Runtime struct {
	index *Index

	// Data holds the legacy index strut. It is for transitional purposes only.
	Data interface{}
}

// New creates a new Runtime.
func New() *Runtime {
	return &Runtime{
		index: NewIndex(SharedIndexNew),
	}
}

func NewWithIndex(x *Index) *Runtime {
	return &Runtime{index: x}
}

func (x *Runtime) IndexToString(i int64) string {
	return x.index.IndexToString(i)
}

func (x *Runtime) StringToIndex(s string) int64 {
	return x.index.StringToIndex(s)
}

func (x *Runtime) Build(b *build.Instance) (v *adt.Vertex, errs errors.Error) {
	if s := b.ImportPath; s != "" {
		// Use cached result, if available.
		if v, err := x.LoadImport(s); v != nil || err != nil {
			return v, err
		}
		// Cache the result if any.
		defer func() {
			if errs == nil && v != nil {
				x.index.AddInst(b.ImportPath, v, b)
			}
		}()
	}

	// Build transitive dependencies.
	for _, file := range b.Files {
		file.VisitImports(func(d *ast.ImportDecl) {
			for _, s := range d.Specs {
				errs = errors.Append(errs, x.buildSpec(b, s))
			}
		})
	}

	if errs != nil {
		return nil, errs
	}

	return compile.Files(nil, x, b.Files...)
}

func (x *Runtime) buildSpec(b *build.Instance, spec *ast.ImportSpec) (errs errors.Error) {
	info, err := astutil.ParseImportSpec(spec)
	if err != nil {
		return errors.Promote(err, "invalid import path")
	}

	pkg := b.LookupImport(info.ID)
	if pkg == nil {
		if strings.Contains(info.ID, ".") {
			return errors.Newf(spec.Pos(),
				"package %q imported but not defined in %s",
				info.ID, b.ImportPath)
		}
		return nil // TODO: check the builtin package exists here.
	}

	if _, err := x.Build(pkg); err != nil {
		return err
	}

	return nil
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
