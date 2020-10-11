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
	"path"
	"sync"

	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal/core/adt"
)

type PackageFunc func(ctx adt.Runtime) (*adt.Vertex, errors.Error)

func RegisterBuiltin(importPath string, f PackageFunc) {
	sharedIndex.RegisterBuiltin(importPath, f)
}

func (x *index) RegisterBuiltin(importPath string, f PackageFunc) {
	if x.builtins == nil {
		x.builtins = map[string]PackageFunc{}
	}
	x.builtins[importPath] = f
}

var SharedRuntime = &Runtime{index: sharedIndex}

func (x *Runtime) IsBuiltinPackage(path string) bool {
	return x.index.isBuiltin(path)
}

// sharedIndex is used for indexing builtins and any other labels common to
// all instances.
var sharedIndex = newIndex()

// index maps conversions from label names to internal codes.
//
// All instances belonging to the same package should share this index.
type index struct {
	// Change this to Instance at some point.
	// From *structLit/*Vertex -> Instance
	imports        map[*adt.Vertex]*build.Instance
	importsByPath  map[string]*adt.Vertex
	importsByBuild map[*build.Instance]*adt.Vertex
	builtins       map[string]PackageFunc

	// mutex     sync.Mutex
	typeCache sync.Map // map[reflect.Type]evaluated

}

func newIndex() *index {
	i := &index{
		imports:        map[*adt.Vertex]*build.Instance{},
		importsByPath:  map[string]*adt.Vertex{},
		importsByBuild: map[*build.Instance]*adt.Vertex{},
	}
	return i
}

func (x *index) isBuiltin(id string) bool {
	if x == nil || x.builtins == nil {
		return false
	}
	_, ok := x.builtins[id]
	return ok
}

func (r *Runtime) AddInst(path string, key *adt.Vertex, p *build.Instance) {
	x := r.index
	if key == nil {
		panic("key must not be nil")
	}
	x.imports[key] = p
	x.importsByBuild[p] = key
	if path != "" {
		x.importsByPath[path] = key
	}
}

func (r *Runtime) GetInstanceFromNode(key *adt.Vertex) *build.Instance {
	return r.index.imports[key]
}

func (r *Runtime) GetNodeFromInstance(key *build.Instance) *adt.Vertex {
	return r.index.importsByBuild[key]
}

func (r *Runtime) LoadImport(importPath string) (*adt.Vertex, errors.Error) {
	x := r.index

	key := x.importsByPath[importPath]
	if key != nil {
		return key, nil
	}

	if x.builtins != nil {
		if f := x.builtins[importPath]; f != nil {
			p, err := f(r)
			if err != nil {
				return adt.ToVertex(&adt.Bottom{Err: err}), nil
			}
			inst := &build.Instance{
				ImportPath: importPath,
				PkgName:    path.Base(importPath),
			}
			x.imports[p] = inst
			x.importsByPath[importPath] = p
			x.importsByBuild[inst] = p
			return p, nil
		}
	}

	return key, nil
}
