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
	"sync"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/core/runtime"
)

// A Runtime is used for creating CUE interpretations.
//
// Any operation that involves two Values or Instances should originate from
// the same Runtime.
//
// The zero value of a Runtime is ready to use.
type Runtime struct {
	ctx *build.Context // TODO: remove
	idx *index
}

func init() {
	internal.GetRuntimeNew = func(instance interface{}) interface{} {
		switch x := instance.(type) {
		case Value:
			return &Runtime{idx: x.idx}

		case *Instance:
			return &Runtime{idx: x.index}

		default:
			panic("argument must be Value or *Instance")
		}
	}

	internal.CheckAndForkRuntimeNew = func(runtime, value interface{}) interface{} {
		r := runtime.(*Runtime)
		idx := value.(Value).ctx().index
		if idx != r.idx {
			panic("value not from same runtime")
		}
		return &Runtime{idx: newIndex(idx)}
	}

	internal.CoreValue = func(value interface{}) (runtime, vertex interface{}) {
		if v, ok := value.(Value); ok && v.v != nil {
			return v.idx.Index, v.v
		}
		return nil, nil
	}
}

func dummyLoad(token.Pos, string) *build.Instance { return nil }

func (r *Runtime) index() *index {
	if r.idx == nil {
		r.idx = newIndex(sharedIndex)
	}
	return r.idx
}

func (r *Runtime) buildContext() *build.Context {
	ctx := r.ctx
	if r.ctx == nil {
		ctx = build.NewContext()
	}
	return ctx
}

func (r *Runtime) complete(p *build.Instance) (*Instance, error) {
	idx := r.index()
	if err := p.Complete(); err != nil {
		return nil, err
	}
	inst := idx.loadInstance(p)
	inst.ImportPath = p.ImportPath
	if inst.Err != nil {
		return nil, inst.Err
	}
	return inst, nil
}

// Compile compiles the given source into an Instance. The source code may be
// provided as a string, byte slice, io.Reader. The name is used as the file
// name in position information. The source may import builtin packages. Use
// Build to allow importing non-builtin packages.
func (r *Runtime) Compile(filename string, source interface{}) (*Instance, error) {
	ctx := r.buildContext()
	p := ctx.NewInstance(filename, dummyLoad)
	if err := p.AddFile(filename, source); err != nil {
		return nil, p.Err
	}
	return r.complete(p)
}

// CompileFile compiles the given source file into an Instance. The source may
// import builtin packages. Use Build to allow importing non-builtin packages.
func (r *Runtime) CompileFile(file *ast.File) (*Instance, error) {
	ctx := r.buildContext()
	p := ctx.NewInstance(file.Filename, dummyLoad)
	err := p.AddSyntax(file)
	if err != nil {
		return nil, err
	}
	_, p.PkgName, _ = internal.PackageInfo(file)
	return r.complete(p)
}

// CompileExpr compiles the given source expression into an Instance. The source
// may import builtin packages. Use Build to allow importing non-builtin
// packages.
func (r *Runtime) CompileExpr(expr ast.Expr) (*Instance, error) {
	f, err := astutil.ToFile(expr)
	if err != nil {
		return nil, err
	}
	return r.CompileFile(f)
}

// Parse parses a CUE source value into a CUE Instance. The source code may
// be provided as a string, byte slice, or io.Reader. The name is used as the
// file name in position information. The source may import builtin packages.
//
// Deprecated: use Compile
func (r *Runtime) Parse(name string, source interface{}) (*Instance, error) {
	return r.Compile(name, source)
}

// Build creates an Instance from the given build.Instance. A returned Instance
// may be incomplete, in which case its Err field is set.
func (r *Runtime) Build(instance *build.Instance) (*Instance, error) {
	return r.complete(instance)
}

// Build creates one Instance for each build.Instance. A returned Instance
// may be incomplete, in which case its Err field is set.
//
// Example:
//	inst := cue.Build(load.Instances(args))
//
func Build(instances []*build.Instance) []*Instance {
	if len(instances) == 0 {
		panic("cue: list of instances must not be empty")
	}
	var r Runtime
	a, _ := r.build(instances)
	return a
}

func (r *Runtime) build(instances []*build.Instance) ([]*Instance, error) {
	index := r.index()

	loaded := []*Instance{}

	var errs errors.Error

	for _, p := range instances {
		_ = p.Complete()
		errs = errors.Append(errs, p.Err)

		i := index.loadInstance(p)
		errs = errors.Append(errs, i.Err)
		loaded = append(loaded, i)
	}

	// TODO: insert imports
	return loaded, errs
}

// FromExpr creates an instance from an expression.
// Any references must be resolved beforehand.
//
// Deprecated: use CompileExpr
func (r *Runtime) FromExpr(expr ast.Expr) (*Instance, error) {
	return r.CompileFile(&ast.File{
		Decls: []ast.Decl{&ast.EmbedDecl{Expr: expr}},
	})
}

// index maps conversions from label names to internal codes.
//
// All instances belonging to the same package should share this index.
type index struct {
	adt.Runtime
	*runtime.Index

	loaded map[*build.Instance]*Instance
	mutex  sync.Mutex
}

// sharedIndex is used for indexing builtins and any other labels common to
// all instances.
var sharedIndex = &index{
	Runtime: runtime.SharedRuntimeNew,
	Index:   runtime.SharedIndexNew,
	loaded:  map[*build.Instance]*Instance{},
}

// NewRuntime creates a *runtime.Runtime with builtins preloaded.
func NewRuntime() *runtime.Runtime {
	idx := runtime.NewIndex(sharedIndex.Index)
	r := runtime.NewWithIndex(idx)
	i := &index{
		Runtime: r,
		Index:   idx,
		loaded:  map[*build.Instance]*Instance{},
	}
	r.Data = i
	return r
}

// newIndex creates a new index.
func newIndex(parent *index) *index {
	idx := runtime.NewIndex(parent.Index)
	r := runtime.NewWithIndex(idx)
	i := &index{
		Runtime: r,
		Index:   idx,
		loaded:  map[*build.Instance]*Instance{},
	}
	r.Data = i
	return i
}

func isBuiltin(s string) bool {
	_, ok := builtins[s]
	return ok
}

func (idx *index) loadInstance(p *build.Instance) *Instance {
	_ = visitInstances(p, func(p *build.Instance, errs errors.Error) errors.Error {
		if inst := idx.loaded[p]; inst != nil {
			if !inst.complete {
				// cycles should be detected by the builder and it should not be
				// possible to construct a build.Instance that has them.
				panic("cue: cycle")
			}
			return inst.Err
		}

		err := runtime.ResolveFiles(idx.Index, p, isBuiltin)
		errs = errors.Append(errs, err)

		v, err := compile.Files(nil, idx.Runtime, p.Files...)
		errs = errors.Append(errs, err)

		inst := newInstance(idx, p, v)
		idx.loaded[p] = inst
		inst.Err = errs

		inst.ImportPath = p.ImportPath
		inst.complete = true

		return inst.Err
	})

	return idx.loaded[p]
}

// TODO: runtime.Runtime has a similar, much simpler, implementation. This
// code should go.

type visitFunc func(b *build.Instance, err errors.Error) (errs errors.Error)

// visitInstances calls f for each transitive dependency.
//
// It passes any errors that occur in transitive dependencies to the visitFunc.
// visitFunc must return the errors it is passed or return nil to ignore it.
func visitInstances(b *build.Instance, f visitFunc) (errs errors.Error) {
	v := visitor{b: b, f: f, errs: b.Err}
	for _, file := range b.Files {
		v.file(file)
	}
	return v.f(b, v.errs)
}

type visitor struct {
	b    *build.Instance
	f    visitFunc
	errs errors.Error
}

func (v *visitor) addErr(e errors.Error) {
	v.errs = errors.Append(v.errs, e)
}

func (v *visitor) file(file *ast.File) {
	file.VisitImports(func(x *ast.ImportDecl) {
		for _, s := range x.Specs {
			v.spec(s)
		}
	})
}

func (v *visitor) spec(spec *ast.ImportSpec) {
	info, err := astutil.ParseImportSpec(spec)
	if err != nil {
		v.addErr(errors.Promote(err, "invalid import path"))
		return
	}

	pkg := v.b.LookupImport(info.ID)
	if pkg == nil {
		if strings.Contains(info.ID, ".") {
			v.addErr(errors.Newf(spec.Pos(),
				"package %q imported but not defined in %s",
				info.ID, v.b.ImportPath))
		}
		return
	}

	v.addErr(visitInstances(pkg, v.f))
}
