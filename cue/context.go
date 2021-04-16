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
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/convert"
	"cuelang.org/go/internal/core/debug"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/core/runtime"
)

// A Context is used for creating CUE Values.
//
// A Context keeps track of loaded instances, indices of internal
// representations of values, and defines the set of supported builtins. Any
// operation that involves two Values should originate from the same Context.
//
// Use
//
//    ctx := cuecontext.New()
//
// to create a new Context.
type Context runtime.Runtime

func (c *Context) runtime() *runtime.Runtime {
	rt := (*runtime.Runtime)(c)
	return rt
}

func (c *Context) ctx() *adt.OpContext {
	return newContext(c.runtime())
}

// Context reports the Context with which this value was created.
func (v Value) Context() *Context {
	return (*Context)(v.idx)
}

// A BuildOption defines options for the various build-related methods of
// Context.
type BuildOption func(o *runtime.Config)

// Scope defines a context in which to resolve unresolved identifiers.
//
// Only one scope may be given. It panics if more than one scope is given
// or if the Context in which scope was created differs from the one where
// this option is used.
func Scope(scope Value) BuildOption {
	return func(o *runtime.Config) {
		if o.Runtime != scope.idx {
			panic("incompatible runtime")
		}
		if o.Scope != nil {
			panic("more than one scope is given")
		}
		o.Scope = scope.v
	}
}

// Filename assigns a filename to parsed content.
func Filename(filename string) BuildOption {
	return func(o *runtime.Config) { o.Filename = filename }
}

func (c *Context) parseOptions(options []BuildOption) (cfg runtime.Config) {
	cfg.Runtime = (*runtime.Runtime)(c)
	for _, f := range options {
		f(&cfg)
	}
	return cfg
}

// BuildInstance creates a Value from the given build.Instance.
//
// The returned Value will represent an error, accessible through Err, if any
// error occurred.
func (c *Context) BuildInstance(i *build.Instance, options ...BuildOption) Value {
	cfg := c.parseOptions(options)
	v, err := c.runtime().Build(&cfg, i)
	if err != nil {
		return c.makeError(err)
	}
	return c.make(v)
}

func (c *Context) makeError(err errors.Error) Value {
	b := &adt.Bottom{Err: err}
	node := &adt.Vertex{BaseValue: b}
	node.UpdateStatus(adt.Finalized)
	node.AddConjunct(adt.MakeRootConjunct(nil, b))
	return c.make(node)
}

// BuildInstances creates a Value for each of the given instances and reports
// the combined errors or nil if there were no errors.
func (c *Context) BuildInstances(instances []*build.Instance) ([]Value, error) {
	var errs errors.Error
	var a []Value
	for _, b := range instances {
		v, err := c.runtime().Build(nil, b)
		if err != nil {
			errs = errors.Append(errs, err)
		}
		a = append(a, c.make(v))
	}
	return a, errs
}

// BuildFile creates a Value from f.
//
// The returned Value will represent an error, accessible through Err, if any
// error occurred.
func (c *Context) BuildFile(f *ast.File, options ...BuildOption) Value {
	cfg := c.parseOptions(options)
	return c.compile(c.runtime().CompileFile(&cfg, f))
}

func (c *Context) compile(v *adt.Vertex, p *build.Instance) Value {
	if p.Err != nil {
		return c.makeError(p.Err)
	}
	return c.make(v)
}

// BuildExpr creates a Value from x.
//
// The returned Value will represent an error, accessible through Err, if any
// error occurred.
func (c *Context) BuildExpr(x ast.Expr, options ...BuildOption) Value {
	cfg := c.parseOptions(options)
	v, p, err := c.runtime().CompileExpr(&cfg, x)
	if err != nil {
		return c.makeError(p.Err)
	}
	return c.make(v)
}

// CompileString parses and build a Value from the given source string.
//
// The returned Value will represent an error, accessible through Err, if any
// error occurred.
func (c *Context) CompileString(src string, options ...BuildOption) Value {
	cfg := c.parseOptions(options)
	return c.compile(c.runtime().Compile(&cfg, src))
}

// ParseString parses and build a Value from the given source bytes.
//
// The returned Value will represent an error, accessible through Err, if any
// error occurred.
func (c *Context) CompileBytes(b []byte, options ...BuildOption) Value {
	cfg := c.parseOptions(options)
	return c.compile(c.runtime().Compile(&cfg, b))
}

// TODO: fs.FS or custom wrapper?
// // CompileFile parses and build a Value from the given source bytes.
// //
// // The returned Value will represent an error, accessible through Err, if any
// // error occurred.
// func (c *Context) CompileFile(f fs.File, options ...BuildOption) Value {
// 	b, err := io.ReadAll(f)
// 	if err != nil {
// 		return c.makeError(errors.Promote(err, "parsing file system file"))
// 	}
// 	return c.compile(c.runtime().Compile("", b))
// }

func (c *Context) make(v *adt.Vertex) Value {
	return newValueRoot(c.runtime(), newContext(c.runtime()), v)
}

// An EncodeOption defines options for the various encoding-related methods of
// Context.
type EncodeOption func(*encodeOptions)

type encodeOptions struct {
	nilIsTop bool
}

func (o *encodeOptions) process(option []EncodeOption) {
	for _, f := range option {
		f(o)
	}
}

// NilIsAny indicates whether a nil value is interpreted as null or _.
//
// The default is to interpret nil as _.
func NilIsAny(isAny bool) EncodeOption {
	return func(o *encodeOptions) { o.nilIsTop = isAny }
}

// Encode converts a Go value to a CUE value.
//
// The returned Value will represent an error, accessible through Err, if any
// error occurred.
func (c *Context) Encode(x interface{}, option ...EncodeOption) Value {
	switch v := x.(type) {
	case adt.Value:
		return newValueRoot(c.runtime(), c.ctx(), v)
	}
	var options encodeOptions
	options.process(option)

	ctx := c.ctx()
	// TODO: is true the right default?
	expr := convert.GoValueToValue(ctx, x, options.nilIsTop)
	n := &adt.Vertex{}
	n.AddConjunct(adt.MakeRootConjunct(nil, expr))
	n.Finalize(ctx)
	return c.make(n)
}

// Encode converts a Go type to a CUE value.
//
// The returned Value will represent an error, accessible through Err, if any
// error occurred.
func (c *Context) EncodeType(x interface{}, option ...EncodeOption) Value {
	switch v := x.(type) {
	case *adt.Vertex:
		return c.make(v)
	}

	ctx := c.ctx()
	expr, err := convert.GoTypeToExpr(ctx, x)
	if err != nil {
		return c.makeError(err)
	}
	n := &adt.Vertex{}
	n.AddConjunct(adt.MakeRootConjunct(nil, expr))
	n.Finalize(ctx)
	return c.make(n)
}

// TODO:

// func (c *Context) NewExpr(op Op, v ...Value) Value {
// 	return Value{}
// }

// func (c *Context) NewList(v ...Value) Value {
// 	return Value{}
// }

// func (c *Context) NewValue(v ...ValueElem) Value {
// 	return Value{}
// }

// func NewAttr(key string, values ...string) *Attribute {
// 	return &Attribute{}
// }

// // Clear unloads all previously-loaded imports.
// func (c *Context) Clear() {
// }

// // Values created up to the point of the Fork will be valid in both runtimes.
// func (c *Context) Fork() *Context {
// 	return nil
// }

// type ValueElem interface {
// }

// func NewField(sel Selector, value Value, attrs ...Attribute) ValueElem {
// 	return nil
// }

// func NewDocComment(text string) ValueElem {
// 	return nil
// }

// newContext returns a new evaluation context.
func newContext(idx *runtime.Runtime) *adt.OpContext {
	if idx == nil {
		return nil
	}
	return eval.NewContext(idx, nil)
}

func debugStr(ctx *adt.OpContext, v adt.Node) string {
	return debug.NodeString(ctx, v, nil)
}

func str(c *adt.OpContext, v adt.Node) string {
	return debugStr(c, v)
}

// eval returns the evaluated value. This may not be the vertex.
//
// Deprecated: use ctx.value
func (v Value) eval(ctx *adt.OpContext) adt.Value {
	if v.v == nil {
		panic("undefined value")
	}
	x := manifest(ctx, v.v)
	return x.Value()
}

// TODO: change from Vertex to Vertex.
func manifest(ctx *adt.OpContext, v *adt.Vertex) *adt.Vertex {
	v.Finalize(ctx)
	return v
}
