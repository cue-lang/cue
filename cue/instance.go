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
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/core/convert"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/core/runtime"
)

// An Instance defines a single configuration based on a collection of
// underlying CUE files.
type Instance struct {
	*index

	root *adt.Vertex

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
	x.Index.AddInst(p.ImportPath, p.root, p)
	p.index = x
	return p
}

func (x *index) getImportFromNode(v *adt.Vertex) *Instance {
	p := x.Index.GetImportFromNode(v)
	if p == nil {
		return nil
	}
	return p.(*Instance)
}

func (x *index) getImportFromPath(id string) *Instance {
	node := x.Index.GetImportFromPath(id)
	if node == nil {
		return nil
	}
	return x.Index.GetImportFromNode(node).(*Instance)
}

func init() {
	internal.MakeInstance = func(value interface{}) interface{} {
		v := value.(Value)
		x := v.eval(v.ctx())
		st, ok := x.(*adt.Vertex)
		if !ok {
			st = &adt.Vertex{}
			st.AddConjunct(adt.MakeRootConjunct(nil, x))
		}
		return v.ctx().index.addInst(&Instance{
			root: st,
		})
	}
}

// newInstance creates a new instance. Use Insert to populate the instance.
func newInstance(x *index, p *build.Instance, v *adt.Vertex) *Instance {
	// TODO: associate root source with structLit.
	i := &Instance{
		root: v,
		inst: p,
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
	v := ctx.manifest(inst.root)
	return v
}

func init() {
	internal.EvalExpr = func(value, expr interface{}) interface{} {
		v := value.(Value)
		e := expr.(ast.Expr)
		ctx := v.idx.newContext()
		return newValueRoot(ctx, evalExpr(ctx, v.vertex(ctx), e))
	}
}

// evalExpr evaluates expr within scope.
func evalExpr(ctx *context, scope *adt.Vertex, expr ast.Expr) evaluated {
	cfg := &compile.Config{
		Scope: scope,
		Imports: func(x *ast.Ident) (pkgPath string) {
			if _, ok := builtins[x.Name]; !ok {
				return ""
			}
			return x.Name
		},
	}

	c, err := compile.Expr(cfg, ctx.opCtx, expr)
	if err != nil {
		return &adt.Bottom{Err: err}
	}
	return adt.Resolve(ctx.opCtx, c)

	// scope.Finalize(ctx.opCtx) // TODO: not appropriate here.
	// switch s := scope.Value.(type) {
	// case *bottom:
	// 	return s
	// case *adt.StructMarker:
	// default:
	// 	return ctx.mkErr(scope, "instance is not a struct, found %s", scope.Kind())
	// }

	// c := ctx.opCtx

	// x, err := compile.Expr(&compile.Config{Scope: scope}, c.Runtime, expr)
	// if err != nil {
	// 	return c.NewErrf("could not evaluate %s: %v", c.Str(x), err)
	// }

	// env := &adt.Environment{Vertex: scope}

	// switch v := x.(type) {
	// case adt.Value:
	// 	return v
	// case adt.Resolver:
	// 	r, err := c.Resolve(env, v)
	// 	if err != nil {
	// 		return err
	// 	}
	// 	return r

	// case adt.Evaluator:
	// 	e, _ := c.Evaluate(env, x)
	// 	return e

	// }

	// return c.NewErrf("could not evaluate %s", c.Str(x))
}

// Doc returns the package comments for this instance.
func (inst *Instance) Doc() []*ast.CommentGroup {
	var docs []*ast.CommentGroup
	if inst.inst == nil {
		return nil
	}
	for _, f := range inst.inst.Files {
		if c := internal.FileComment(f); c != nil {
			docs = append(docs, c)
		}
	}
	return docs
}

// Value returns the root value of the configuration. If the configuration
// defines in emit value, it will be that value. Otherwise it will be all
// top-level values.
func (inst *Instance) Value() Value {
	ctx := inst.newContext()
	inst.root.Finalize(ctx.opCtx)
	return newVertexRoot(ctx, inst.root)
}

// Eval evaluates an expression within an existing instance.
//
// Expressions may refer to builtin packages if they can be uniquely identified.
func (inst *Instance) Eval(expr ast.Expr) Value {
	ctx := inst.newContext()
	v := inst.root
	v.Finalize(ctx.opCtx)
	result := evalExpr(ctx, v, expr)
	return newValueRoot(ctx, result)
}

// Merge unifies the given instances into a single one.
//
// Errors regarding conflicts are included in the result, but not reported, so
// that these will only surface during manifestation. This allows
// non-conflicting parts to be used.
func Merge(inst ...*Instance) *Instance {
	v := &adt.Vertex{}

	i := inst[0]
	ctx := i.index.newContext().opCtx

	// TODO: interesting test: use actual unification and then on K8s corpus.

	for _, i := range inst {
		w := i.Value()
		v.AddConjunct(adt.MakeRootConjunct(nil, w.v.ToDataAll()))
	}
	v.Finalize(ctx)

	p := i.index.addInst(&Instance{
		root:     v,
		complete: true,
	})
	return p
}

// Build creates a new instance from the build instances, allowing unbound
// identifier to bind to the top-level field in inst. The top-level fields in
// inst take precedence over predeclared identifier and builtin functions.
func (inst *Instance) Build(p *build.Instance) *Instance {
	p.Complete()

	idx := inst.index
	r := inst.index.Runtime

	rErr := runtime.ResolveFiles(idx.Index, p, isBuiltin)

	v, err := compile.Files(&compile.Config{Scope: inst.root}, r, p.Files...)

	v.AddConjunct(adt.MakeRootConjunct(nil, inst.root))

	i := newInstance(idx, p, v)
	if rErr != nil {
		i.setListOrError(rErr)
	}
	if i.Err != nil {
		i.setListOrError(i.Err)
	}

	if err != nil {
		i.setListOrError(err)
	}

	i.complete = true

	return i
}

func (inst *Instance) value() Value {
	return newVertexRoot(inst.newContext(), inst.root)
}

// Lookup reports the value at a path starting from the top level struct. The
// Exists method of the returned value will report false if the path did not
// exist. The Err method reports if any error occurred during evaluation. The
// empty path returns the top-level configuration struct. Use LookupDef for definitions or LookupField for
// any kind of field.
func (inst *Instance) Lookup(path ...string) Value {
	return inst.value().Lookup(path...)
}

// LookupDef reports the definition with the given name within struct v. The
// Exists method of the returned value will report false if the definition did
// not exist. The Err method reports if any error occurred during evaluation.
func (inst *Instance) LookupDef(path string) Value {
	return inst.value().LookupDef(path)
}

// LookupField reports a Field at a path starting from v, or an error if the
// path is not. The empty path returns v itself.
//
// It cannot look up hidden or unexported fields.
//
// Deprecated: this API does not work with new-style definitions. Use
// FieldByName defined on inst.Value().
func (inst *Instance) LookupField(path ...string) (f FieldInfo, err error) {
	v := inst.value()
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

// Fill creates a new instance with the values of the old instance unified with
// the given value. It is not possible to update the emit value.
//
// Values may be any Go value that can be converted to CUE, an ast.Expr or
// a Value. In the latter case, it will panic if the Value is not from the same
// Runtime.
func (inst *Instance) Fill(x interface{}, path ...string) (*Instance, error) {
	for i := len(path) - 1; i >= 0; i-- {
		x = map[string]interface{}{path[i]: x}
	}
	a := make([]adt.Conjunct, len(inst.root.Conjuncts))
	copy(a, inst.root.Conjuncts)
	u := &adt.Vertex{Conjuncts: a}

	if v, ok := x.(Value); ok {
		if inst.index != v.idx {
			panic("value of type Value is not created with same Runtime as Instance")
		}
		for _, c := range v.v.Conjuncts {
			u.AddConjunct(c)
		}
	} else {
		ctx := eval.NewContext(inst.index.Runtime, nil)
		expr := convert.GoValueToExpr(ctx, true, x)
		u.AddConjunct(adt.MakeRootConjunct(nil, expr))
		u.Finalize(ctx)
	}
	inst = inst.index.addInst(&Instance{
		root: u,
		inst: nil,

		// Omit ImportPath to indicate this is not an importable package.
		Dir:        inst.Dir,
		PkgName:    inst.PkgName,
		Incomplete: inst.Incomplete,

		complete: true,
	})
	return inst, nil
}
