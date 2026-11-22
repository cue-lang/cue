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
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/core/runtime"
)

// An InstanceOrValue is implemented by [Value] and [*Instance].
//
// This is a placeholder type that is used to allow Instance-based APIs to
// transition to Value-based APIs. The goals is to get rid of the Instance
// type before v1.0.0.
type InstanceOrValue interface {
	Value() Value

	internal()
}

func (Value) internal()     {}
func (*Instance) internal() {}

// Value implements [InstanceOrValue].
func (v hiddenValue) Value() Value { return v }

// An Instance defines a single configuration based on a collection of
// underlying CUE files.
//
// Use of this type is being phased out in favor of [Value].
// Any APIs currently taking an Instance should use [InstanceOrValue]
// to transition to the new type without breaking users.
type Instance struct {
	index *runtime.Runtime

	root *adt.Vertex

	ImportPath  string
	Dir         string
	PkgName     string
	DisplayName string

	Incomplete bool         // true if Pkg and all its dependencies are free of errors
	Err        errors.Error // non-nil if the package had errors

	inst *build.Instance
}

type hiddenInstance = Instance

func addInst(x *runtime.Runtime, p *Instance) *Instance {
	if p.inst == nil {
		p.inst = &build.Instance{
			ImportPath: p.ImportPath,
			PkgName:    p.PkgName,
		}
	}
	x.AddInst(p.root, p.inst)
	x.SetBuildData(p.inst, p)
	p.index = x
	return p
}

func lookupInstance(x *runtime.Runtime, p *build.Instance) *Instance {
	if x, ok := x.BuildData(p); ok {
		return x.(*Instance)
	}
	return nil
}

// getImportFromBuild returns the instance for p with root v. If it creates the
// instance, it records errs on it, the errors of the build of v, or those of p
// if errs is nil.
func getImportFromBuild(x *runtime.Runtime, p *build.Instance, v *adt.Vertex, errs errors.Error) *Instance {
	inst := lookupInstance(x, p)
	if inst != nil {
		return inst
	}

	inst = newUncachedInstance(x, p, v, errs)
	x.SetBuildData(p, inst)
	return inst
}

// newUncachedInstance is like [getImportFromBuild], without using the cache.
func newUncachedInstance(x *runtime.Runtime, p *build.Instance, v *adt.Vertex, errs errors.Error) *Instance {
	inst := &Instance{
		ImportPath:  p.ImportPath,
		Dir:         p.Dir,
		PkgName:     p.PkgName,
		DisplayName: p.ImportPath,
		root:        v,
		inst:        p,
		index:       x,
	}
	if errs == nil {
		errs = p.Err
	}
	if errs != nil {
		inst.setListOrError(errs)
	}
	return inst
}

// enclosingPackage returns the root of the package which encloses v, which
// may be v itself, and its build instance, or nils if there is none.
func enclosingPackage(x *runtime.Runtime, v *adt.Vertex) (*adt.Vertex, *build.Instance) {
	for ; v != nil; v = v.Parent {
		if p := x.GetInstanceFromNode(v); p != nil {
			return v, p
		}
	}
	return nil, nil
}

// packageSyntaxInfo returns the ID and language version of the package which
// encloses v, without creating an [Instance], which the runtime would cache.
func packageSyntaxInfo(x *runtime.Runtime, v *adt.Vertex) (id, langVersion string) {
	if _, p := enclosingPackage(x, v); p != nil {
		return p.ID(), p.TargetLanguageVersion()
	}
	return "", ""
}

func getImportFromNode(x *runtime.Runtime, v *adt.Vertex) *Instance {
	root, p := enclosingPackage(x, v)
	if p == nil {
		return nil
	}
	if inst := lookupInstance(x, p); inst != nil {
		return inst
	}
	inst := newUncachedInstance(x, p, root, nil)
	// An instance which the runtime does not index must not outlive its
	// root, so only cache the others.
	if x.LoadInstance(p) != nil {
		x.SetBuildData(p, inst)
	}
	return inst
}

// newInstance creates a new instance. Use Insert to populate the instance.
func newInstance(x *runtime.Runtime, p *build.Instance, v *adt.Vertex) *Instance {
	// TODO: associate root source with structLit.
	inst := &Instance{
		root: v,
		inst: p,
	}
	if p != nil {
		inst.ImportPath = p.ImportPath
		inst.Dir = p.Dir
		inst.PkgName = p.PkgName
		inst.DisplayName = p.ImportPath
		if p.Err != nil {
			inst.setListOrError(p.Err)
		}
	}

	x.AddInst(v, p)
	x.SetBuildData(p, inst)
	inst.index = x
	return inst
}

func (inst *Instance) setListOrError(err errors.Error) {
	inst.Incomplete = true
	inst.Err = errors.Append(inst.Err, err)
}

// ID returns the package identifier that uniquely qualifies module and
// package name.
func (inst *Instance) ID() string {
	if inst == nil || inst.inst == nil {
		return ""
	}
	return inst.inst.ID()
}

// Value returns the root value of the configuration. If the configuration
// defines in emit value, it will be that value. Otherwise it will be all
// top-level values.
func (inst *Instance) Value() Value {
	ctx := newContext(inst.index)
	// TODO: consider including these statistics as well. Right now, this only
	// seems to be used in cue cmd for "auxiliary" evaluations, like filetypes.
	// These arguably skew the actual statistics for the cue command line, so
	// it is convenient to not include these.
	// adt.AddStats(ctx)
	return newVertexRoot(inst.index, ctx, inst.root)
}

// Deprecated: do not use; use unification instead.
func Merge(inst ...*Instance) *Instance {
	v := &adt.Vertex{}

	i := inst[0]
	ctx := newContext(i.index)

	// TODO: interesting test: use actual unification and then on K8s corpus.

	for _, i := range inst {
		w := i.Value()
		v.AddConjunct(adt.MakeRootConjunct(nil, w.v.ToDataAll(ctx)))
	}
	v.Finalize(ctx)

	p := addInst(i.index, &Instance{
		root: v,
	})
	return p
}

// Build creates a new instance from the build instances, allowing unbound
// identifier to bind to the top-level field in inst. The top-level fields in
// inst take precedence over predeclared identifier and builtin functions.
//
// Deprecated: use [Context.BuildInstance]
func (inst *hiddenInstance) Build(p *build.Instance) *Instance {
	p.Complete()

	idx := inst.index
	r := inst.index

	cfg := &compile.Config{Scope: valueScope(Value{idx: r, v: inst.root})}
	v, err := compile.Instance(cfg, r, p)

	// Just like [runtime.Runtime.Build], ensure that the @embed injector is run as needed.
	err = errors.Append(err, r.InjectImplementations(p, v))

	v.AddConjunct(adt.MakeRootConjunct(nil, inst.root))

	i := newInstance(idx, p, v)
	if p.ResolutionErr != nil {
		i.setListOrError(p.ResolutionErr)
	}
	if i.Err != nil {
		i.setListOrError(i.Err)
	}

	if err != nil {
		i.setListOrError(err)
	}

	return i
}
