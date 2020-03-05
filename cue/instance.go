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
	goast "go/ast"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
)

// An Instance defines a single configuration based on a collection of
// underlying CUE files.
type Instance struct {
	*index

	rootStruct *structLit // the struct to insert root values into
	rootValue  value      // the value to evaluate: may add comprehensions

	// scope is used as an additional top-level scope between the package scope
	// and the predeclared identifiers.
	scope *structLit

	ImportPath  string
	Dir         string
	PkgName     string
	DisplayName string

	Incomplete bool         // true if Pkg and all its dependencies are free of errors
	Err        errors.Error // non-nil if the package had errors

	inst *build.Instance

	complete bool // for cycle detection
}

func (x *index) addInst(p *Instance) *Instance {
	if p.rootStruct == nil {
		panic("struct must not be nil")
	}
	p.index = x
	x.imports[p.rootStruct] = p
	if p.ImportPath != "" {
		x.importsByPath[p.ImportPath] = p
	}
	return p
}

func (x *index) getImportFromNode(v value) *Instance {
	imp := x.imports[v]
	if imp == nil && x.parent != nil {
		return x.parent.getImportFromNode(v)
	}
	return imp
}

func init() {
	internal.MakeInstance = func(value interface{}) interface{} {
		v := value.(Value)
		x := v.eval(v.ctx())
		st, ok := x.(*structLit)
		if !ok {
			st = &structLit{baseValue: x.base(), emit: x}
		}
		return v.ctx().index.addInst(&Instance{
			rootStruct: st,
			rootValue:  v.path.v,
		})
	}
}

// newInstance creates a new instance. Use Insert to populate the instance.
func (x *index) newInstance(p *build.Instance) *Instance {
	// TODO: associate root source with structLit.
	st := &structLit{baseValue: baseValue{nil}}
	i := &Instance{
		rootStruct: st,
		rootValue:  st,
		inst:       p,
	}
	if p != nil {
		i.ImportPath = p.ImportPath
		i.Dir = p.Dir
		i.PkgName = p.PkgName
		i.DisplayName = p.ImportPath
		if p.Err != nil {
			i.setListOrError(p.Err)
		}
	}
	return x.addInst(i)
}

func (inst *Instance) setListOrError(err errors.Error) {
	inst.Incomplete = true
	inst.Err = errors.Append(inst.Err, err)
}

func (inst *Instance) setError(err errors.Error) {
	inst.Incomplete = true
	inst.Err = errors.Append(inst.Err, err)
}

func (inst *Instance) eval(ctx *context) evaluated {
	// TODO: remove manifest here?
	v := ctx.manifest(inst.rootValue)
	if s, ok := v.(*structLit); ok && s.emit != nil {
		e := s.emit.evalPartial(ctx)
		src := binSrc(token.NoPos, opUnify, v, e)
	outer:
		switch e.(type) {
		case *structLit, *top:
			v = binOp(ctx, src, opUnifyUnchecked, v, e)
			if s, ok := v.(*structLit); ok {
				s.emit = nil
			}

		default:
			for _, a := range s.arcs {
				if !a.definition {
					v = binOp(ctx, src, opUnify, v, e)
					break outer
				}
			}
			return e
		}
	}
	return v
}

func init() {
	internal.EvalExpr = func(value, expr interface{}) interface{} {
		v := value.(Value)
		e := expr.(ast.Expr)
		ctx := v.idx.newContext()
		return newValueRoot(ctx, evalExpr(v.idx, v.eval(ctx), e))
	}
}

func evalExpr(idx *index, x value, expr ast.Expr) evaluated {
	if isBottom(x) {
		return idx.mkErr(x, "error evaluating instance: %v", x)
	}
	obj, ok := x.(*structLit)
	if !ok {
		return idx.mkErr(obj, "instance is not a struct")
	}

	v := newVisitor(idx, nil, nil, obj, true)
	return eval(idx, v.walk(expr))
}

func (inst *Instance) evalExpr(ctx *context, expr ast.Expr) evaluated {
	root := inst.eval(ctx)
	if isBottom(root) {
		return ctx.mkErr(root, "error evaluating instance")
	}
	obj, ok := root.(*structLit)
	if !ok {
		return ctx.mkErr(root, "instance is not a struct, found %s",
			root.kind())
	}
	v := newVisitor(ctx.index, inst.inst, nil, obj, true)
	return v.walk(expr).evalPartial(ctx)
}

// Doc returns the package comments for this instance.
func (inst *Instance) Doc() []*ast.CommentGroup {
	var docs []*ast.CommentGroup
	if inst.inst == nil {
		return nil
	}
	for _, f := range inst.inst.Files {
		pkg, _, _ := internal.PackageInfo(f)
		if pkg == nil {
			continue
		}
		var cg *ast.CommentGroup
		for _, c := range pkg.Comments() {
			if c.Position == 0 {
				cg = c
			}
		}
		if cg != nil {
			docs = append(docs, cg)
		}
	}
	return docs
}

// Value returns the root value of the configuration. If the configuration
// defines in emit value, it will be that value. Otherwise it will be all
// top-level values.
func (inst *Instance) Value() Value {
	ctx := inst.newContext()
	return newValueRoot(ctx, inst.eval(ctx))
}

// Eval evaluates an expression within an existing instance.
//
// Expressions may refer to builtin packages if they can be uniquely identified.
func (inst *Instance) Eval(expr ast.Expr) Value {
	ctx := inst.newContext()
	result := inst.evalExpr(ctx, expr)
	return newValueRoot(ctx, result)
}

// Merge unifies the given instances into a single one.
//
// Errors regarding conflicts are included in the result, but not reported, so
// that these will only surface during manifestation. This allows
// non-conflicting parts to be used.
func Merge(inst ...*Instance) *Instance {
	switch len(inst) {
	case 0:
		return nil
	case 1:
		return inst[0]
	}

	values := []value{}
	for _, i := range inst {
		if i.Err != nil {
			return i
		}
		values = append(values, i.rootValue)
	}
	merged := &mergedValues{values: values}

	ctx := inst[0].newContext()

	st, ok := ctx.manifest(merged).(*structLit)
	if !ok {
		return nil
	}

	p := ctx.index.addInst(&Instance{
		rootStruct: st,
		rootValue:  merged,
		complete:   true,
	})
	return p
}

// Build creates a new instance from the build instances, allowing unbound
// identifier to bind to the top-level field in inst. The top-level fields in
// inst take precedence over predeclared identifier and builtin functions.
func (inst *Instance) Build(p *build.Instance) *Instance {
	p.Complete()

	idx := inst.index

	i := idx.newInstance(p)
	if i.Err != nil {
		return i
	}

	ctx := inst.newContext()
	val := newValueRoot(ctx, inst.rootValue)
	v, err := val.structValFull(ctx)
	if err != nil {
		i.setError(val.toErr(err))
		return i
	}
	i.scope = v.obj

	if err := resolveFiles(idx, p); err != nil {
		i.setError(err)
		return i
	}
	for _, f := range p.Files {
		if err := i.insertFile(f); err != nil {
			i.setListOrError(err)
		}
	}
	i.complete = true

	return i
}

// Lookup reports the value at a path starting from the top level struct (not
// the emitted value). The Exists method of the returned value will report false
// if the path did not exist. The Err method reports if any error occurred
// during evaluation. The empty path returns the top-level configuration struct,
// regardless of whether an emit value was specified.
// Use LookupDef for definitions or LookupField for any kind of field.
func (inst *Instance) Lookup(path ...string) Value {
	idx := inst.index
	ctx := idx.newContext()
	v := newValueRoot(ctx, inst.rootValue)
	for _, k := range path {
		obj, err := v.structValData(ctx)
		if err != nil {
			return Value{idx, &valueData{arc: arc{cache: err, v: err}}}
		}
		v = obj.Lookup(k)
	}
	return v
}

// LookupDef reports the definition with the given name within struct v. The
// Exists method of the returned value will report false if the definition did
// not exist. The Err method reports if any error occurred during evaluation.
func (inst *Instance) LookupDef(path string) Value {
	ctx := inst.index.newContext()
	v := newValueRoot(ctx, inst.rootValue.evalPartial(ctx))
	return v.LookupDef(path)
}

// LookupField reports a Field at a path starting from v, or an error if the
// path is not. The empty path returns v itself.
//
// It cannot look up hidden or unexported fields.
func (inst *Instance) LookupField(path ...string) (f FieldInfo, err error) {
	idx := inst.index
	ctx := idx.newContext()
	v := newValueRoot(ctx, inst.rootValue)
	for i, k := range path {
		s, err := v.Struct()
		if err != nil {
			return f, err
		}

		f, err = s.FieldByName(k)
		if err != nil {
			return f, err
		}
		if f.IsHidden || (i == 0 || f.IsDefinition) && !goast.IsExported(f.Name) {
			return f, errNotFound
		}
		v = f.Value
	}
	return f, err
}

// Fill creates a new instance with the values of the old instance unified with
// the given value. It is not possible to update the emit value.
//
// Values may be any Go value that can be converted to CUE, an ast.Expr or
// a Value. In the latter case, it will panic if the Value is not from the same
// Runtime.
func (inst *Instance) Fill(x interface{}, path ...string) (*Instance, error) {
	ctx := inst.newContext()
	root := ctx.manifest(inst.rootValue)
	for i := len(path) - 1; i >= 0; i-- {
		x = map[string]interface{}{path[i]: x}
	}
	var value evaluated
	if v, ok := x.(Value); ok {
		if inst.index != v.ctx().index {
			panic("value of type Value is not created with same Runtime as Instance")
		}
		value = v.eval(ctx)
	} else {
		value = convert(ctx, root, true, x)
	}
	eval := binOp(ctx, baseValue{}, opUnify, root, value)
	// TODO: validate recursively?
	err := inst.Err
	var st *structLit
	var stVal evaluated
	switch x := eval.(type) {
	case *structLit:
		st = x
		stVal = x
	default:
		// This should not happen.
		b := ctx.mkErr(eval, "error filling struct")
		err = inst.Value().toErr(b)
		st = &structLit{emit: b}
		stVal = b
	case *bottom:
		err = inst.Value().toErr(x)
		st = &structLit{emit: x}
		stVal = x
	}
	inst = inst.index.addInst(&Instance{
		rootStruct: st,
		rootValue:  stVal,
		inst:       nil,

		// Omit ImportPath to indicate this is not an importable package.
		Dir:        inst.Dir,
		PkgName:    inst.PkgName,
		Incomplete: inst.Incomplete,
		Err:        err,

		complete: err != nil,
	})
	return inst, err
}
