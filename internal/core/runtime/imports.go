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

func RegisterBuiltin(inst *build.Instance, f PackageFunc) {
	stdBuiltins.registerBuiltin(inst, f)
}

var stdBuiltins = &builtins{
	importPaths: make(map[string]*build.Instance),
	instances:   make(map[*build.Instance]func(*Runtime) (*adt.Vertex, error)),
	shortNames:  make(map[string]*build.Instance),
}

// builtins defines a set of builtin packages (usually the CUE standard library).
type builtins struct {
	importPaths map[string]*build.Instance
	instances   map[*build.Instance]func(*Runtime) (*adt.Vertex, error)
	shortNames  map[string]*build.Instance
}

func (b *builtins) registerBuiltin(inst *build.Instance, f PackageFunc) {
	b.importPaths[inst.ImportPath] = inst
	b.instances[inst] = func(r *Runtime) (*adt.Vertex, error) {
		return f(r)
	}
	base := path.Base(inst.ImportPath)
	if _, ok := b.shortNames[base]; ok {
		b.shortNames[base] = nil // Don't allow ambiguous base paths.
	} else {
		b.shortNames[base] = inst
	}
}

// BuiltinPackagePath converts a short-form builtin package identifier to its
// full path or "" if this doesn't exist.
func (x *Runtime) BuiltinPackagePath(ident string) string {
	inst := x.BuiltinPackageInstance(ident)
	if inst == nil {
		return ""
	}
	return inst.ImportPath
}

// BuiltinPackageInstance converts a short-form builtin package identifier to its
// build instance or nil if this doesn't exist.
func (x *Runtime) BuiltinPackageInstance(ident string) *build.Instance {
	if x.index.builtins == nil {
		return nil
	}
	return x.index.builtins.shortNames[ident]
}

// index caches the results of converting [build.Instance]
// values to ADT nodes, and also defines the namespace
// of builtin packages.
type index struct {
	builtins *builtins

	// lock is used to guard imports-related maps.
	lock           sync.RWMutex
	imports        map[*adt.Vertex]*build.Instance
	importsByBuild map[*build.Instance]*adt.Vertex

	nextUniqueID uint64
	typeCache    sync.Map // map[reflect.Type]evaluated
}

func (i *index) getNextUniqueID() uint64 {
	// TODO: use atomic increment instead.
	i.lock.Lock()
	i.nextUniqueID++
	x := i.nextUniqueID
	i.lock.Unlock()
	return x
}

func newIndex() *index {
	i := &index{
		imports:        map[*adt.Vertex]*build.Instance{},
		importsByBuild: map[*build.Instance]*adt.Vertex{},
	}
	return i
}

func (r *Runtime) AddInst(key *adt.Vertex, p *build.Instance) {
	r.index.lock.Lock()
	defer r.index.lock.Unlock()

	x := r.index
	if key == nil {
		panic("key must not be nil")
	}
	x.imports[key] = p
	x.importsByBuild[p] = key
}

func (r *Runtime) GetInstanceFromNode(key *adt.Vertex) *build.Instance {
	r.index.lock.RLock()
	defer r.index.lock.RUnlock()

	return r.index.imports[key]
}

func (r *Runtime) getNodeFromInstance(key *build.Instance) *adt.Vertex {
	r.index.lock.RLock()
	defer r.index.lock.RUnlock()

	return r.index.importsByBuild[key]
}

// LoadBuiltin loads the builtin package with the given import path.
func (r *Runtime) LoadBuiltin(importPath string) *adt.Vertex {
	x := r.index
	inst := x.builtins.importPaths[importPath]
	if inst == nil {
		return nil
	}
	if v := r.LoadInstance(inst); v != nil {
		return v
	}
	x.lock.Lock()
	defer x.lock.Unlock()

	if v := x.importsByBuild[inst]; v != nil {
		// Another goroutine got there first.
		return v
	}
	v, err := x.builtins.instances[inst](r)
	if err != nil {
		// TODO why not just cache the error, or even just have the
		// builtin builder return *adt.Bottom?
		return adt.ToVertex(&adt.Bottom{Err: errors.Promote(err, "builtin")})
	}
	x.importsByBuild[inst] = v
	x.imports[v] = inst
	return v
}

func (r *Runtime) LoadInstance(inst *build.Instance) *adt.Vertex {
	r.index.lock.RLock()
	defer r.index.lock.RUnlock()
	return r.index.importsByBuild[inst]
}
