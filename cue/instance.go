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
	x.AddInst(p.ImportPath, p.root, p.inst)
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

func getImportFromBuild(x *runtime.Runtime, p *build.Instance, v *adt.Vertex) *Instance {
	inst := lookupInstance(x, p)

	if inst != nil {
		return inst
	}

	inst = &Instance{
		ImportPath:  p.ImportPath,
		Dir:         p.Dir,
		PkgName:     p.PkgName,
		DisplayName: p.ImportPath,
		root:        v,
		inst:        p,
		index:       x,
	}
	if p.Err != nil {
		inst.setListOrError(p.Err)
	}

	x.SetBuildData(p, inst)

	return inst
}

func getImportFromNode(x *runtime.Runtime, v *adt.Vertex) *Instance {
	p := x.GetInstanceFromNode(v)
	if p == nil {
		return nil
	}

	return getImportFromBuild(x, p, v)
}

func getImportFromPath(x *runtime.Runtime, id string) *Instance {
	node := x.LoadImport(id)
	if node == nil {
		return nil
	}
	b := x.GetInstanceFromNode(node)
	inst := lookupInstance(x, b)
	if inst == nil {
		inst = &Instance{
			ImportPath: b.ImportPath,
			PkgName:    b.PkgName,
			root:       node,
			inst:       b,
			index:      x,
		}
	}
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
	inst.root.Finalize(ctx)
	// TODO: consider including these statistics as well. Right now, this only
	// seems to be used in cue cmd for "auxiliary" evaluations, like filetypes.
	// These arguably skew the actual statistics for the cue command line, so
	// it is convenient to not include these.
	// adt.AddStats(ctx)
	return newVertexRoot(inst.index, ctx, inst.root)
}

// Lookup reports the value at a path starting from the top level struct. The
// Exists method of the returned value will report false if the path did not
// exist. The Err method reports if any error occurred during evaluation. The
// empty path returns the top-level configuration struct. Use LookupDef for definitions or LookupField for
// any kind of field.
//
// Deprecated: use [Value.LookupPath]
func (inst *hiddenInstance) Lookup(path ...string) Value {
	return inst.Value().Lookup(path...)
}

// LookupDef reports the definition with the given name within struct v. The
// Exists method of the returned value will report false if the definition did
// not exist. The Err method reports if any error occurred during evaluation.
//
// Deprecated: use [Value.LookupPath]
func (inst *hiddenInstance) LookupDef(path string) Value {
	return inst.Value().LookupDef(path)
}

// LookupField reports a Field at a path starting from v, or an error if the
// path is not. The empty path returns v itself.
//
// It cannot look up hidden or unexported fields.
//
// Deprecated: use [Value.LookupPath]
func (inst *hiddenInstance) LookupField(path ...string) (f FieldInfo, err error) {
	v := inst.Value()
	for _, k := range path {
		s, err := v.Struct()
		if err != nil {
			return f, err
		}

		f, err = s.FieldByName(k, true)
		if err != nil {
			return f, err
		}
		if f.IsHidden {
			return f, errNotFound
		}
		v = f.Value
	}
	return f, err
}
