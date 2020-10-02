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

package adt

import (
	"fmt"
	"reflect"
	"regexp"

	"github.com/cockroachdb/apd/v2"
	"golang.org/x/text/encoding/unicode"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/token"
)

// A Unifier implements a strategy for CUE's unification operation. It must
// handle the following aspects of CUE evaluation:
//
//    - Structural and reference cycles
//    - Non-monotic validation
//    - Fixed-point computation of comprehension
//
type Unifier interface {
	// Unify fully unifies all values of a Vertex to completion and stores
	// the result in the Vertex. If Unify was called on v before it returns
	// the cached results.
	Unify(c *OpContext, v *Vertex, state VertexStatus) // error or bool?

	// Evaluate returns the evaluated value associated with v. It may return a
	// partial result. That is, if v was not yet unified, it may return a
	// concrete value that must be the result assuming the configuration has no
	// errors.
	//
	// This semantics allows CUE to break reference cycles in a straightforward
	// manner.
	//
	// Vertex v must still be evaluated at some point to catch the underlying
	// error.
	//
	Evaluate(c *OpContext, v *Vertex) Value
}

// Runtime defines an interface for low-level representation conversion and
// lookup.
type Runtime interface {
	// StringIndexer allows for converting string labels to and from a
	// canonical numeric representation.
	StringIndexer

	// LoadImport loads a unique Vertex associated with a given import path. It
	// returns an error if no import for this package could be found.
	LoadImport(importPath string) (*Vertex, errors.Error)

	// StoreType associates a CUE expression with a Go type.
	StoreType(t reflect.Type, src ast.Expr, expr Expr)

	// LoadType retrieves a previously stored CUE expression for a given Go
	// type if available.
	LoadType(t reflect.Type) (src ast.Expr, expr Expr, ok bool)
}

type Config struct {
	Runtime
	Unifier

	Format func(Node) string
}

type config = Config

// New creates an operation context.
func New(v *Vertex, cfg *Config) *OpContext {
	if cfg.Runtime == nil {
		panic("nil Runtime")
	}
	if cfg.Unifier == nil {
		panic("nil Unifier")
	}
	ctx := &OpContext{
		config: *cfg,
		vertex: v,
	}
	if v != nil {
		ctx.e = &Environment{Up: nil, Vertex: v}
	}
	return ctx
}

// An OpContext associates a Runtime and Unifier to allow evaluating the types
// defined in this package. It tracks errors provides convenience methods for
// evaluating values.
type OpContext struct {
	config

	e         *Environment
	src       ast.Node
	errs      *Bottom
	positions []Node // keep track of error positions

	// vertex is used to determine the path location in case of error. Turning
	// this into a stack could also allow determining the cyclic path for
	// structural cycle errors.
	vertex *Vertex

	// TODO: remove use of tentative. Should be possible if incomplete
	// handling is done better.
	tentative int // set during comprehension evaluation
}

// Impl is for internal use only. This will go.
func (c *OpContext) Impl() Runtime {
	return c.config.Runtime
}

// If IsTentative is set, evaluation of an arc should not finalize
// to non-concrete values.
func (c *OpContext) IsTentative() bool {
	return c.tentative > 0
}

func (c *OpContext) Pos() token.Pos {
	if c.src == nil {
		return token.NoPos
	}
	return c.src.Pos()
}

func (c *OpContext) Source() ast.Node {
	return c.src
}

// NewContext creates an operation context.
func NewContext(r Runtime, u Unifier, v *Vertex) *OpContext {
	return New(v, &Config{Runtime: r, Unifier: u})
}

func (c *OpContext) pos() token.Pos {
	if c.src == nil {
		return token.NoPos
	}
	return c.src.Pos()
}

func (c *OpContext) spawn(node *Vertex) *OpContext {
	sub := *c
	node.Parent = c.e.Vertex
	sub.e = &Environment{Up: c.e, Vertex: node}
	return &sub
}

func (c *OpContext) Env(upCount int32) *Environment {
	e := c.e
	for ; upCount > 0; upCount-- {
		e = e.Up
	}
	return e
}

func (c *OpContext) relNode(upCount int32) *Vertex {
	e := c.e
	for ; upCount > 0; upCount-- {
		e = e.Up
	}
	c.Unify(c, e.Vertex, Partial)
	return e.Vertex
}

func (c *OpContext) relLabel(upCount int32) Feature {
	// locate current label.
	e := c.e
	for ; upCount > 0; upCount-- {
		e = e.Up
	}
	return e.DynamicLabel
}

func (c *OpContext) concreteIsPossible(x Expr) bool {
	if v, ok := x.(Value); ok {
		if v.Concreteness() > Concrete {
			c.AddErrf("value can never become concrete")
			return false
		}
	}
	return true
}

// HasErr reports whether any error was reported, including whether value
// was incomplete.
func (c *OpContext) HasErr() bool {
	return c.errs != nil
}

func (c *OpContext) Err() *Bottom {
	b := c.errs
	c.errs = nil
	return b
}

func (c *OpContext) addErrf(code ErrorCode, pos token.Pos, msg string, args ...interface{}) {
	for i, a := range args {
		switch x := a.(type) {
		case Node:
			args[i] = c.Str(x)
		case ast.Node:
			b, _ := format.Node(x)
			args[i] = string(b)
		case Feature:
			args[i] = x.SelectorString(c.Runtime)
		}
	}

	err := c.NewPosf(pos, msg, args...)
	c.addErr(code, err)
}

func (c *OpContext) addErr(code ErrorCode, err errors.Error) {
	c.errs = CombineErrors(c.src, c.errs, &Bottom{Code: code, Err: err})
}

// AddBottom records an error in OpContext.
func (c *OpContext) AddBottom(b *Bottom) {
	c.errs = CombineErrors(c.src, c.errs, b)
}

// AddErr records an error in OpContext. It returns errors collected so far.
func (c *OpContext) AddErr(err errors.Error) *Bottom {
	if err != nil {
		c.errs = CombineErrors(c.src, c.errs, &Bottom{Err: err})
	}
	return c.errs
}

// NewErrf creates a *Bottom value and returns it. The returned uses the
// current source as the point of origin of the error.
func (c *OpContext) NewErrf(format string, args ...interface{}) *Bottom {
	// TODO: consider renaming ot NewBottomf: this is now confusing as we also
	// have Newf.
	err := c.Newf(format, args...)
	return &Bottom{Src: c.src, Err: err, Code: EvalError}
}

// AddErrf records an error in OpContext. It returns errors collected so far.
func (c *OpContext) AddErrf(format string, args ...interface{}) *Bottom {
	return c.AddErr(c.Newf(format, args...))
}

func (c *OpContext) validate(v Value) *Bottom {
	switch x := v.(type) {
	case *Bottom:
		return x
	case *Vertex:
		v := c.Unifier.Evaluate(c, x)
		if b, ok := v.(*Bottom); ok {
			return b
		}
	}
	return nil
}

type frame struct {
	env *Environment
	err *Bottom
	src ast.Node
}

func (c *OpContext) PushState(env *Environment, src ast.Node) (saved frame) {
	saved.env = c.e
	saved.err = c.errs
	saved.src = c.src

	c.errs = nil
	if src != nil {
		c.src = src
	}
	c.e = env

	return saved
}

func (c *OpContext) PopState(s frame) *Bottom {
	err := c.errs
	c.e = s.env
	c.errs = s.err
	c.src = s.src
	return err
}

// PushArc signals c that arc v is currently being processed for the purpose
// of error reporting. PopArc should be called with the returned value once
// processing of v is completed.
func (c *OpContext) PushArc(v *Vertex) (saved *Vertex) {
	c.vertex, saved = v, c.vertex
	return saved
}

// PopArc signals completion of processing the current arc.
func (c *OpContext) PopArc(saved *Vertex) {
	c.vertex = saved
}

// Resolve finds a node in the tree.
//
// Should only be used to insert Conjuncts. TODO: perhaps only return Conjuncts
// and error.
func (c *OpContext) Resolve(env *Environment, r Resolver) (*Vertex, *Bottom) {
	s := c.PushState(env, r.Source())

	arc := r.resolve(c)
	// TODO: check for cycle errors?

	err := c.PopState(s)
	if err != nil {
		return nil, err
	}

	return arc, err
}

// Validate calls validates value for the given validator.
//
// TODO(errors): return boolean instead: only the caller has enough information
// to generate a proper error message.
func (c *OpContext) Validate(check Validator, value Value) *Bottom {
	// TODO: use a position stack to push both values.
	saved := c.src
	c.src = check.Source()

	err := check.validate(c, value)

	c.src = saved

	return err
}

// Yield evaluates a Yielder and calls f for each result.
func (c *OpContext) Yield(env *Environment, y Yielder, f YieldFunc) *Bottom {
	s := c.PushState(env, y.Source())

	c.tentative++

	y.yield(c, f)

	c.tentative--

	return c.PopState(s)

}

// Concrete returns the concrete value of x after evaluating it.
// msg is used to mention the context in which an error occurred, if any.
func (c *OpContext) Concrete(env *Environment, x Expr, msg interface{}) (result Value, complete bool) {

	v, complete := c.Evaluate(env, x)

	v, ok := c.getDefault(v)
	if !ok {
		return v, false
	}

	if !IsConcrete(v) {
		complete = false
		b := c.NewErrf("non-concrete value %v in operand to %s", c.Str(v), msg)
		b.Code = IncompleteError
		v = b
	}

	if !complete {
		return v, complete
	}

	return v, true
}

func (c *OpContext) getDefault(v Value) (result Value, ok bool) {
	var d *Disjunction
	switch x := v.(type) {
	default:
		return v, true

	case *Vertex:
		switch t := x.Value.(type) {
		case *Disjunction:
			d = t

		case *StructMarker, *ListMarker:
			return v, true

		default:
			return t, true
		}

	case *Disjunction:
		d = x
	}

	if d.NumDefaults != 1 {
		c.addErrf(IncompleteError, c.pos(),
			"unresolved disjunction %s (type %s)", c.Str(d), d.Kind())
		return nil, false
	}
	return c.getDefault(d.Values[0])
}

// Evaluate evaluates an expression within the given environment and indicates
// whether the result is complete. It will always return a non-nil result.
func (c *OpContext) Evaluate(env *Environment, x Expr) (result Value, complete bool) {
	s := c.PushState(env, x.Source())

	val := c.eval(x)

	complete = true

	if err, _ := val.(*Bottom); err != nil && err.IsIncomplete() {
		complete = false
	}
	if val == nil {
		complete = false
		// TODO ENSURE THIS DOESN"T HAPPEN>
		val = &Bottom{
			Code: IncompleteError,
			Err:  c.Newf("UNANTICIPATED ERROR"),
		}

	}

	_ = c.PopState(s)

	if !complete || val == nil {
		return val, false
	}

	return val, true
}

// value evaluates expression v within the current environment. The result may
// be nil if the result is incomplete. value leaves errors untouched to that
// they can be collected by the caller.
func (c *OpContext) value(x Expr) (result Value) {
	v := c.evalState(x, Partial)

	v, _ = c.getDefault(v)
	return v
}

func (c *OpContext) eval(v Expr) (result Value) {
	return c.evalState(v, Partial)
}

func (c *OpContext) evalState(v Expr, state VertexStatus) (result Value) {
	savedSrc := c.src
	c.src = v.Source()
	err := c.errs
	c.errs = nil

	defer func() {
		c.errs = CombineErrors(c.src, c.errs, err)
		c.errs = CombineErrors(c.src, c.errs, result)
		if c.errs != nil {
			result = c.errs
		}
		c.src = savedSrc
	}()

	switch x := v.(type) {
	case Value:
		return x

	case Evaluator:
		v := x.evaluate(c)
		return v

	case Resolver:
		arc := x.resolve(c)
		if c.HasErr() {
			return nil
		}
		if isIncomplete(arc) {
			if arc != nil {
				return arc.Value
			}
			return nil
		}

		v := c.Unifier.Evaluate(c, arc)
		return v

	default:
		// return nil
		c.AddErrf("unexpected Expr type %T", v)
	}
	return nil
}

func (c *OpContext) lookup(x *Vertex, pos token.Pos, l Feature) *Vertex {
	if l == InvalidLabel || x == nil {
		// TODO: is it possible to have an invalid label here? Maybe through the
		// API?
		return &Vertex{}
	}

	var kind Kind
	if x.Value != nil {
		kind = x.Value.Kind()
	}

	switch kind {
	case StructKind:
		if l.Typ() == IntLabel {
			c.addErrf(0, pos, "invalid struct selector %s (type int)", l)
		}

	case ListKind:
		switch {
		case l.Typ() == IntLabel:
			switch {
			case l.Index() < 0:
				c.addErrf(0, pos, "invalid list index %s (index must be non-negative)", l)
				return nil
			case l.Index() > len(x.Arcs):
				c.addErrf(0, pos, "invalid list index %s (out of bounds)", l)
				return nil
			}

		case l.IsDef():

		default:
			c.addErrf(0, pos, "invalid list index %s (type string)", l)
			return nil
		}

	default:
		// TODO: ?
		// if !l.IsDef() {
		// 	c.addErrf(0, nil, "invalid selector %s (must be definition for non-structs)", l)
		// }
	}

	a := x.Lookup(l)
	if a == nil {
		code := IncompleteError
		if !x.Accept(c, l) {
			code = 0
		}
		// TODO: if the struct was a literal struct, we can also treat it as
		// closed and make this a permanent error.
		label := l.SelectorString(c.Runtime)
		c.addErrf(code, pos, "undefined field %s", label)
	}
	return a
}

func (c *OpContext) Label(x Value) Feature {
	return labelFromValue(c, x)
}

func (c *OpContext) typeError(v Value, k Kind) {
	if isError(v) {
		return
	}
	if !IsConcrete(v) && v.Kind()&k != 0 {
		c.addErrf(IncompleteError, pos(v),
			"incomplete %s value '%s'", k, c.Str(v))
	} else {
		c.AddErrf("cannot use %s (type %s) as type %s", c.Str(v), v.Kind(), k)
	}
}

func (c *OpContext) typeErrorAs(v Value, k Kind, as interface{}) {
	if as == nil {
		c.typeError(v, k)
		return
	}
	if isError(v) {
		return
	}
	if !IsConcrete(v) && v.Kind()&k != 0 {
		c.addErrf(IncompleteError, pos(v),
			"incomplete %s value '%s' in as", k, c.Str(v), as)
	} else {
		c.AddErrf("cannot use %s (type %s) as type %s in %v",
			c.Str(v), v.Kind(), k, as)
	}
}

var emptyNode = &Vertex{}

func pos(x Node) token.Pos {
	if x.Source() == nil {
		return token.NoPos
	}
	return x.Source().Pos()
}

func (c *OpContext) node(x Expr, state VertexStatus) *Vertex {
	v := c.evalState(x, state)

	v, ok := c.getDefault(v)
	if !ok {
		// Error already generated by getDefault.
		return emptyNode
	}

	node, ok := v.(*Vertex)
	if !ok {
		if isError(v) {
			if v == nil {
				c.addErrf(IncompleteError, pos(x), "incomplete value %s", c.Str(x))
				return emptyNode
			}
		}
		if v.Kind()&StructKind != 0 {
			c.addErrf(IncompleteError, pos(x),
				"incomplete feed source value %s (type %s)",
				x.Source(), v.Kind())
		} else if b, ok := v.(*Bottom); ok {
			c.AddBottom(b)
		} else {
			c.addErrf(0, pos(x), // TODO(error): better message.
				"invalid operand %s (found %s, want list or struct)",
				x.Source(), v.Kind())

		}
		return emptyNode
	}
	return node.Default()
}

// Elems returns the elements of a list.
func (c *OpContext) Elems(v Value) []*Vertex {
	list := c.list(v)
	return list.Elems()
}

func (c *OpContext) list(v Value) *Vertex {
	x, ok := v.(*Vertex)
	if !ok || !x.IsList() {
		c.typeError(v, ListKind)
		return emptyNode
	}
	return x
}

func (c *OpContext) scalar(v Value) Value {
	v = Unwrap(v)
	switch v.(type) {
	case *Null, *Bool, *Num, *String, *Bytes:
	default:
		c.typeError(v, ScalarKinds)
	}
	return v
}

var zero = &Num{K: NumKind}

func (c *OpContext) num(v Value, as interface{}) *Num {
	v = Unwrap(v)
	if isError(v) {
		return zero
	}
	x, ok := v.(*Num)
	if !ok {
		c.typeErrorAs(v, NumKind, as)
		return zero
	}
	return x
}

func (c *OpContext) Int64(v Value) int64 {
	v = Unwrap(v)
	if isError(v) {
		return 0
	}
	x, ok := v.(*Num)
	if !ok {
		c.typeError(v, IntKind)
		return 0
	}
	i, err := x.X.Int64()
	if err != nil {
		c.AddErrf("number is not an int64: %v", err)
		return 0
	}
	return i
}

func (c *OpContext) uint64(v Value, as string) uint64 {
	v = Unwrap(v)
	if isError(v) {
		return 0
	}
	x, ok := v.(*Num)
	if !ok {
		c.typeErrorAs(v, IntKind, as)
		return 0
	}
	if x.X.Negative {
		// TODO: improve message
		c.AddErrf("cannot convert negative number to uint64")
		return 0
	}
	if !x.X.Coeff.IsUint64() {
		// TODO: improve message
		c.AddErrf("cannot convert number %s to uint64", x.X)
		return 0
	}
	return x.X.Coeff.Uint64()
}

func (c *OpContext) BoolValue(v Value) bool {
	return c.boolValue(v, nil)
}

func (c *OpContext) boolValue(v Value, as interface{}) bool {
	v = Unwrap(v)
	if isError(v) {
		return false
	}
	x, ok := v.(*Bool)
	if !ok {
		c.typeErrorAs(v, BoolKind, as)
		return false
	}
	return x.B
}

func (c *OpContext) StringValue(v Value) string {
	return c.stringValue(v, nil)
}

// ToBytes returns the bytes value of a scalar value.
func (c *OpContext) ToBytes(v Value) []byte {
	if x, ok := v.(*Bytes); ok {
		return x.B
	}
	return []byte(c.ToString(v))
}

// ToString returns the string value of a scalar value.
func (c *OpContext) ToString(v Value) string {
	return c.toStringValue(v, StringKind|NumKind|BytesKind|BoolKind, nil)

}

func (c *OpContext) stringValue(v Value, as interface{}) string {
	return c.toStringValue(v, StringKind, as)
}

func (c *OpContext) toStringValue(v Value, k Kind, as interface{}) string {
	v = Unwrap(v)
	if isError(v) {
		return ""
	}
	if v.Kind()&k == 0 {
		if as == nil {
			c.typeError(v, k)
		} else {
			c.typeErrorAs(v, k, as)
		}
		return ""
	}
	switch x := v.(type) {
	case *String:
		return x.Str

	case *Bytes:
		return bytesToString(x.B)

	case *Num:
		return x.X.String()

	case *Bool:
		if x.B {
			return "true"
		}
		return "false"

	default:
		c.addErrf(IncompleteError, c.pos(),
			"non-concrete value %s (type %s)", c.Str(v), v.Kind())
	}
	return ""
}

func bytesToString(b []byte) string {
	b, _ = unicode.UTF8.NewDecoder().Bytes(b)
	return string(b)
}

func (c *OpContext) bytesValue(v Value, as interface{}) []byte {
	v = Unwrap(v)
	if isError(v) {
		return nil
	}
	x, ok := v.(*Bytes)
	if !ok {
		c.typeErrorAs(v, BytesKind, as)
		return nil
	}
	return x.B
}

var matchNone = regexp.MustCompile("^$")

func (c *OpContext) regexp(v Value) *regexp.Regexp {
	v = Unwrap(v)
	if isError(v) {
		return matchNone
	}
	switch x := v.(type) {
	case *String:
		if x.RE != nil {
			return x.RE
		}
		// TODO: synchronization
		p, err := regexp.Compile(x.Str)
		if err != nil {
			// FatalError? How to cache error
			c.AddErrf("invalid regexp: %s", err)
			x.RE = matchNone
		} else {
			x.RE = p
		}
		return x.RE

	case *Bytes:
		if x.RE != nil {
			return x.RE
		}
		// TODO: synchronization
		p, err := regexp.Compile(string(x.B))
		if err != nil {
			c.AddErrf("invalid regexp: %s", err)
			x.RE = matchNone
		} else {
			x.RE = p
		}
		return x.RE

	default:
		c.typeError(v, StringKind|BytesKind)
		return matchNone
	}
}

func (c *OpContext) newNum(d *apd.Decimal, k Kind, sources ...Node) Value {
	if c.HasErr() {
		return c.Err()
	}
	return &Num{Src: c.src, X: *d, K: k}
}

func (c *OpContext) NewInt64(n int64, sources ...Node) Value {
	if c.HasErr() {
		return c.Err()
	}
	d := apd.New(n, 0)
	return &Num{Src: c.src, X: *d, K: IntKind}
}

func (c *OpContext) NewString(s string) Value {
	if c.HasErr() {
		return c.Err()
	}
	return &String{Src: c.src, Str: s}
}

func (c *OpContext) newBytes(b []byte) Value {
	if c.HasErr() {
		return c.Err()
	}
	return &Bytes{Src: c.src, B: b}
}

func (c *OpContext) newBool(b bool) Value {
	if c.HasErr() {
		return c.Err()
	}
	return &Bool{Src: c.src, B: b}
}

func (c *OpContext) newList(src ast.Node, parent *Vertex) *Vertex {
	return &Vertex{Parent: parent, Value: &ListMarker{}}
}

// Str reports a debug string of x.
func (c *OpContext) Str(x Node) string {
	if c.Format == nil {
		return fmt.Sprintf("%T", x)
	}
	return c.Format(x)
}

// NewList returns a new list for the given values.
func (c *OpContext) NewList(values ...Value) *Vertex {
	// TODO: consider making this a literal list instead.
	list := &ListLit{}
	v := &Vertex{
		Conjuncts: []Conjunct{{Env: nil, x: list}},
	}

	for _, x := range values {
		list.Elems = append(list.Elems, x)
	}
	c.Unify(c, v, Finalized)
	return v
}
