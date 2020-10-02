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
	"bytes"
	"fmt"
	"io"
	"regexp"

	"github.com/cockroachdb/apd/v2"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
)

// A StructLit represents an unevaluated struct literal or file body.
type StructLit struct {
	Src   ast.Node // ast.File or ast.StructLit
	Decls []Decl

	// administrative fields like hasreferences.
	// hasReferences bool
}

func (x *StructLit) Source() ast.Node { return x.Src }

func (x *StructLit) evaluate(c *OpContext) Value {
	e := c.Env(0)
	v := &Vertex{Conjuncts: []Conjunct{{e, x, 0}}}
	c.Unifier.Unify(c, v, Finalized) // TODO: also partial okay?
	return v
}

// FIELDS
//
// Fields can also be used as expressions whereby the value field is the
// expression this allows retaining more context.

// Field represents a field with a fixed label. It can be a regular field,
// definition or hidden field.
//
//   foo: bar
//   #foo: bar
//   _foo: bar
//
// Legacy:
//
//   Foo :: bar
//
type Field struct {
	Src *ast.Field

	Label Feature
	Value Expr
}

func (x *Field) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

// An OptionalField represents an optional regular field.
//
//   foo?: expr
//
type OptionalField struct {
	Src   *ast.Field
	Label Feature
	Value Expr
}

func (x *OptionalField) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

// A BulkOptionalField represents a set of optional field.
//
//   [expr]: expr
//
type BulkOptionalField struct {
	Src    *ast.Field // Elipsis or Field
	Filter Expr
	Value  Expr
	Label  Feature // for reference and formatting
}

func (x *BulkOptionalField) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

// A Ellipsis represents a set of optional fields of a given type.
//
//   ...T
//
type Ellipsis struct {
	Src   *ast.Ellipsis
	Value Expr
}

func (x *Ellipsis) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

// A DynamicField represents a regular field for which the key is computed.
//
//    "\(expr)": expr
//    (expr): expr
//
type DynamicField struct {
	Src   *ast.Field
	Key   Expr
	Value Expr
}

func (x *DynamicField) IsOptional() bool {
	return x.Src.Optional != token.NoPos
}

func (x *DynamicField) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

// A ListLit represents an unevaluated list literal.
//
//    [a, for x in src { ... }, b, ...T]
//
type ListLit struct {
	Src *ast.ListLit

	// scalars, comprehensions, ...T
	Elems []Elem
}

func (x *ListLit) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *ListLit) evaluate(c *OpContext) Value {
	e := c.Env(0)
	v := &Vertex{Conjuncts: []Conjunct{{e, x, 0}}}
	c.Unifier.Unify(c, v, Finalized) // TODO: also partial okay?
	return v
}

// Null represents null. It can be used as a Value and Expr.
type Null struct {
	Src ast.Node
}

func (x *Null) Source() ast.Node { return x.Src }
func (x *Null) Kind() Kind       { return NullKind }

// Bool is a boolean value. It can be used as a Value and Expr.
type Bool struct {
	Src ast.Node
	B   bool
}

func (x *Bool) Source() ast.Node { return x.Src }
func (x *Bool) Kind() Kind       { return BoolKind }

// Num is a numeric value. It can be used as a Value and Expr.
type Num struct {
	Src ast.Node
	K   Kind        // needed?
	X   apd.Decimal // Is integer if the apd.Decimal is an integer.
}

// TODO: do we need this?
// func NewNumFromString(src ast.Node, s string) Value {
// 	n := &Num{Src: src, K: IntKind}
// 	if strings.ContainsAny(s, "eE.") {
// 		n.K = FloatKind
// 	}
// 	_, _, err := n.X.SetString(s)
// 	if err != nil {
// 		pos := token.NoPos
// 		if src != nil {
// 			pos = src.Pos()
// 		}
// 		return &Bottom{Err: errors.Newf(pos, "invalid number: %v", err)}
// 	}
// 	return n
// }

func (x *Num) Source() ast.Node { return x.Src }
func (x *Num) Kind() Kind       { return x.K }

// TODO: do we still need this?
// func (x *Num) Specialize(k Kind) Value {
// 	k = k & x.K
// 	if k == x.K {
// 		return x
// 	}
// 	y := *x
// 	y.K = k
// 	return &y
// }

// String is a string value. It can be used as a Value and Expr.
type String struct {
	Src ast.Node
	Str string
	RE  *regexp.Regexp // only set if needed
}

func (x *String) Source() ast.Node { return x.Src }
func (x *String) Kind() Kind       { return StringKind }

// Bytes is a bytes value. It can be used as a Value and Expr.
type Bytes struct {
	Src ast.Node
	B   []byte
	RE  *regexp.Regexp // only set if needed
}

func (x *Bytes) Source() ast.Node { return x.Src }
func (x *Bytes) Kind() Kind       { return BytesKind }

// Composites: the evaluated fields of a composite are recorded in the arc
// vertices.

type ListMarker struct {
	Src    ast.Node
	IsOpen bool
}

func (x *ListMarker) Source() ast.Node { return x.Src }
func (x *ListMarker) Kind() Kind       { return ListKind }
func (x *ListMarker) node()            {}

type StructMarker struct {
	// NeedClose is used to signal that the evaluator should close this struct.
	// It is only set by the close builtin.
	NeedClose bool
}

func (x *StructMarker) Source() ast.Node { return nil }
func (x *StructMarker) Kind() Kind       { return StructKind }
func (x *StructMarker) node()            {}

// Top represents all possible values. It can be used as a Value and Expr.
type Top struct{ Src *ast.Ident }

func (x *Top) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}
func (x *Top) Kind() Kind { return TopKind }

// BasicType represents all values of a certain Kind. It can be used as a Value
// and Expr.
//
//   string
//   int
//   num
//   bool
//
type BasicType struct {
	Src *ast.Ident
	K   Kind
}

func (x *BasicType) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}
func (x *BasicType) Kind() Kind { return x.K }

// TODO: do we still need this?
// func (x *BasicType) Specialize(k Kind) Value {
// 	k = x.K & k
// 	if k == x.K {
// 		return x
// 	}
// 	y := *x
// 	y.K = k
// 	return &y
// }

// TODO: should we use UnaryExpr for Bound now we have BoundValue?

// BoundExpr represents an unresolved unary comparator.
//
//    <a
//    =~MyPattern
//
type BoundExpr struct {
	Src  *ast.UnaryExpr
	Op   Op
	Expr Expr
}

func (x *BoundExpr) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *BoundExpr) evaluate(ctx *OpContext) Value {
	if v, ok := x.Expr.(Value); ok {
		if v == nil || v.Concreteness() > Concrete {
			return ctx.NewErrf("bound has fixed non-concrete value")
		}
		return &BoundValue{x.Src, x.Op, v}
	}
	v := ctx.value(x.Expr)
	if isError(v) {
		return v
	}
	if v.Concreteness() > Concrete {
		ctx.addErrf(IncompleteError, ctx.pos(),
			"non-concrete value %s for bound %s", ctx.Str(x.Expr), x.Op)
		return nil
	}
	return &BoundValue{x.Src, x.Op, v}
}

// A BoundValue is a fully evaluated unary comparator that can be used to
// validate other values.
//
//    <5
//    =~"Name$"
//
type BoundValue struct {
	Src   ast.Expr
	Op    Op
	Value Value
}

func (x *BoundValue) Source() ast.Node { return x.Src }
func (x *BoundValue) Kind() Kind {
	k := x.Value.Kind()
	switch k {
	case IntKind, FloatKind, NumKind:
		return NumKind

	case NullKind:
		if x.Op == NotEqualOp {
			return TopKind &^ NullKind
		}
	}
	return k
}

func (x *BoundValue) validate(c *OpContext, y Value) *Bottom {
	a := y // Can be list or struct.
	b := c.scalar(x.Value)
	if c.HasErr() {
		return c.Err()
	}

	switch v := BinOp(c, x.Op, a, b).(type) {
	case *Bottom:
		return v

	case *Bool:
		if v.B {
			return nil
		}
		// TODO(errors): use "invalid value %v (not an %s)" if x is a
		// predeclared identifier such as `int`.
		return c.NewErrf("invalid value %v (out of bound %s)",
			c.Str(y), c.Str(x))

	default:
		panic(fmt.Sprintf("unsupported type %T", v))
	}
}

// A NodeLink is used during computation to refer to an existing Vertex.
// It is used to signal a potential cycle or reference.
// Note that a NodeLink may be used as a value. This should be taken into
// account.
type NodeLink struct {
	Node *Vertex
}

func (x *NodeLink) Kind() Kind {
	return x.Node.Kind()
}
func (x *NodeLink) Source() ast.Node             { return x.Node.Source() }
func (x *NodeLink) resolve(c *OpContext) *Vertex { return x.Node }

// A FieldReference represents a lexical reference to a field.
//
//    a
//
type FieldReference struct {
	Src     *ast.Ident
	UpCount int32
	Label   Feature
}

func (x *FieldReference) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *FieldReference) resolve(c *OpContext) *Vertex {
	n := c.relNode(x.UpCount)
	pos := pos(x)
	return c.lookup(n, pos, x.Label)
}

// A LabelReference refers to the string or integer value of a label.
//
//    [X=Pattern]: b: X
//
type LabelReference struct {
	Src     *ast.Ident
	UpCount int32
}

// TODO: should this implement resolver at all?

func (x *LabelReference) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *LabelReference) evaluate(ctx *OpContext) Value {
	label := ctx.relLabel(x.UpCount)
	if label == 0 {
		// There is no label. This may happen if a LabelReference is evaluated
		// outside of the context of a parent node, for instance if an
		// "additional" items or properties is evaluated in isolation.
		//
		// TODO: this should return the pattern of the label.
		return &BasicType{K: StringKind}
	}
	return label.ToValue(ctx)
}

// A DynamicReference is like a LabelReference, but with a computed label.
//
//    X=(x): X
//    X="\(x)": X
//
type DynamicReference struct {
	Src     *ast.Ident
	UpCount int32
	Label   Expr

	// TODO: only use aliases and store the actual expression only in the scope.
	// The feature is unique for every instance. This will also allow dynamic
	// fields to be ordered among normal fields.
	//
	// This could also be used to assign labels to embedded values, if they
	// don't match a label.
	Alias Feature
}

func (x *DynamicReference) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *DynamicReference) resolve(ctx *OpContext) *Vertex {
	e := ctx.Env(x.UpCount)
	frame := ctx.PushState(e, x.Src)
	v := ctx.value(x.Label)
	ctx.PopState(frame)
	f := ctx.Label(v)
	return ctx.lookup(e.Vertex, pos(x), f)
}

// An ImportReference refers to an imported package.
//
//    import "strings"
//
//    strings.ToLower("Upper")
//
type ImportReference struct {
	Src        *ast.Ident
	ImportPath Feature
	Label      Feature // for informative purposes
}

func (x *ImportReference) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *ImportReference) resolve(ctx *OpContext) *Vertex {
	path := x.ImportPath.StringValue(ctx)
	v, _ := ctx.Runtime.LoadImport(path)
	return v
}

// A LetReference evaluates a let expression in its original environment.
//
//   let X = x
//
type LetReference struct {
	Src     *ast.Ident
	UpCount int32
	Label   Feature // for informative purposes
	X       Expr
}

func (x *LetReference) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *LetReference) resolve(c *OpContext) *Vertex {
	e := c.Env(x.UpCount)
	label := e.Vertex.Label
	// Anonymous arc.
	return &Vertex{Parent: nil, Label: label, Conjuncts: []Conjunct{{e, x.X, 0}}}
}

func (x *LetReference) evaluate(c *OpContext) Value {
	e := c.Env(x.UpCount)

	// Not caching let expressions may lead to exponential behavior.
	return e.evalCached(c, x.X)
}

// A SelectorExpr looks up a fixed field in an expression.
//
//     X.Sel
//
type SelectorExpr struct {
	Src *ast.SelectorExpr
	X   Expr
	Sel Feature
}

func (x *SelectorExpr) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *SelectorExpr) resolve(c *OpContext) *Vertex {
	n := c.node(x.X, Partial)
	return c.lookup(n, x.Src.Sel.Pos(), x.Sel)
}

// IndexExpr is like a selector, but selects an index.
//
//    X[Index]
//
type IndexExpr struct {
	Src   *ast.IndexExpr
	X     Expr
	Index Expr
}

func (x *IndexExpr) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *IndexExpr) resolve(ctx *OpContext) *Vertex {
	// TODO: support byte index.
	n := ctx.node(x.X, Partial)
	i := ctx.value(x.Index)
	f := ctx.Label(i)
	return ctx.lookup(n, x.Src.Index.Pos(), f)
}

// A SliceExpr represents a slice operation. (Not currently in spec.)
//
//    X[Lo:Hi:Stride]
//
type SliceExpr struct {
	Src    *ast.SliceExpr
	X      Expr
	Lo     Expr
	Hi     Expr
	Stride Expr
}

func (x *SliceExpr) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *SliceExpr) evaluate(c *OpContext) Value {
	// TODO: strides

	v := c.value(x.X)
	const as = "slice index"

	switch v := v.(type) {
	case nil:
		c.addErrf(IncompleteError, c.pos(),
			"non-concrete slice subject %s", c.Str(x.X))
		return nil
	case *Vertex:
		if !v.IsList() {
			break
		}

		var (
			lo = uint64(0)
			hi = uint64(len(v.Arcs))
		)
		if x.Lo != nil {
			lo = c.uint64(c.value(x.Lo), as)
		}
		if x.Hi != nil {
			hi = c.uint64(c.value(x.Hi), as)
			if hi > uint64(len(v.Arcs)) {
				return c.NewErrf("index %d out of range", hi)
			}
		}
		if lo > hi {
			return c.NewErrf("invalid slice index: %d > %d", lo, hi)
		}

		n := c.newList(c.src, v.Parent)
		for i, a := range v.Arcs[lo:hi] {
			label, err := MakeLabel(a.Source(), int64(i), IntLabel)
			if err != nil {
				c.AddBottom(&Bottom{Src: a.Source(), Err: err})
				return nil
			}
			n.Arcs = append(n.Arcs, &Vertex{
				Label:     label,
				Conjuncts: a.Conjuncts,
			})
		}
		n.status = Finalized
		return n

	case *Bytes:
		var (
			lo = uint64(0)
			hi = uint64(len(v.B))
		)
		if x.Lo != nil {
			lo = c.uint64(c.value(x.Lo), as)
		}
		if x.Hi != nil {
			hi = c.uint64(c.value(x.Hi), as)
			if hi > uint64(len(v.B)) {
				return c.NewErrf("index %d out of range", hi)
			}
		}
		if lo > hi {
			return c.NewErrf("invalid slice index: %d > %d", lo, hi)
		}
		return c.newBytes(v.B[lo:hi])
	}

	if isError(v) {
		return v
	}
	return c.NewErrf("cannot slice %v (type %s)", c.Str(v), v.Kind())
}

// An Interpolation is a string interpolation.
//
//    "a \(b) c"
//
type Interpolation struct {
	Src   *ast.Interpolation
	K     Kind   // string or bytes
	Parts []Expr // odd: strings, even sources
}

func (x *Interpolation) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *Interpolation) evaluate(c *OpContext) Value {
	buf := bytes.Buffer{}
	for _, e := range x.Parts {
		v := c.value(e)
		if x.K == BytesKind {
			buf.Write(c.ToBytes(v))
		} else {
			buf.WriteString(c.ToString(v))
		}
	}
	if err := c.Err(); err != nil {
		err = &Bottom{
			Code: err.Code,
			Err:  errors.Wrapf(err.Err, pos(x), "invalid interpolation"),
		}
		// c.AddBottom(err)
		// return nil
		return err
	}
	if x.K == BytesKind {
		return &Bytes{x.Src, buf.Bytes(), nil}
	}
	return &String{x.Src, buf.String(), nil}
}

// UnaryExpr is a unary expression.
//
//    Op X
//    -X !X +X
//
type UnaryExpr struct {
	Src *ast.UnaryExpr
	Op  Op
	X   Expr
}

func (x *UnaryExpr) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *UnaryExpr) evaluate(c *OpContext) Value {
	if !c.concreteIsPossible(x.X) {
		return nil
	}
	v := c.value(x.X)
	if isError(v) {
		return v
	}

	op := x.Op
	k := kind(v)
	expectedKind := k
	switch op {
	case SubtractOp:
		if v, ok := v.(*Num); ok {
			f := *v
			f.X.Neg(&v.X)
			f.Src = x.Src
			return &f
		}
		expectedKind = NumKind

	case AddOp:
		if v, ok := v.(*Num); ok {
			// TODO: wrap in thunk to save position of '+'?
			return v
		}
		expectedKind = NumKind

	case NotOp:
		if v, ok := v.(*Bool); ok {
			return &Bool{x.Src, !v.B}
		}
		expectedKind = BoolKind
	}
	if k&expectedKind != BottomKind {
		c.addErrf(IncompleteError, pos(x.X),
			"operand %s of '%s' not concrete (was %s)", c.Str(x.X), op, k)
		return nil
	}
	return c.NewErrf("invalid operation %s (%s %s)", c.Str(x), op, k)
}

// BinaryExpr is a binary expression.
//
//    X + Y
//    X & Y
//
type BinaryExpr struct {
	Src *ast.BinaryExpr
	Op  Op
	X   Expr
	Y   Expr
}

func (x *BinaryExpr) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *BinaryExpr) evaluate(c *OpContext) Value {
	env := c.Env(0)
	if x.Op == AndOp {
		// Anonymous Arc
		v := &Vertex{Conjuncts: []Conjunct{{env, x, 0}}}
		c.Unifier.Unify(c, v, Finalized)
		return v
	}

	if !c.concreteIsPossible(x.X) || !c.concreteIsPossible(x.Y) {
		return nil
	}

	left, _ := c.Concrete(env, x.X, x.Op)
	right, _ := c.Concrete(env, x.Y, x.Op)

	leftKind := kind(left)
	rightKind := kind(right)

	// TODO: allow comparing to a literal Bottom only. Find something more
	// principled perhaps. One should especially take care that two values
	// evaluating to Bottom don't evaluate to true. For now we check for
	// Bottom here and require that one of the values be a Bottom literal.
	if isLiteralBottom(x.X) || isLiteralBottom(x.Y) {
		if b := c.validate(left); b != nil {
			left = b
		}
		if b := c.validate(right); b != nil {
			right = b
		}
		switch x.Op {
		case EqualOp:
			return &Bool{x.Src, leftKind == rightKind}
		case NotEqualOp:
			return &Bool{x.Src, leftKind != rightKind}
		}
	}

	if err := CombineErrors(x.Src, left, right); err != nil {
		return err
	}

	if err := c.Err(); err != nil {
		return err
	}

	value := BinOp(c, x.Op, left, right)
	if n, ok := value.(*Vertex); ok && n.IsList() {
		n.UpdateStatus(Partial)
	}
	return value
}

// A CallExpr represents a call to a builtin.
//
//    len(x)
//    strings.ToLower(x)
//
type CallExpr struct {
	Src  *ast.CallExpr
	Fun  Expr
	Args []Expr
}

func (x *CallExpr) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *CallExpr) evaluate(c *OpContext) Value {
	fun := c.value(x.Fun)
	args := []Value{}
	for _, a := range x.Args {
		expr := c.value(a)
		if v, ok := expr.(*Vertex); ok {
			// Remove the path of the origin for arguments. This results in
			// more sensible error messages: an error should refer to the call
			// site, not the original location of the argument.
			// TODO: alternative, explicitly mark the argument number and use
			// that in error messages.
			w := *v
			w.Parent = nil
			args = append(args, &w)
		} else {
			args = append(args, expr)
		}
	}
	if c.HasErr() {
		return nil
	}
	b, _ := fun.(*Builtin)
	if b == nil {
		c.addErrf(0, pos(x.Fun), "cannot call non-function %s (type %s)",
			c.Str(x.Fun), kind(fun))
		return nil
	}
	result := b.call(c, x.Src, args)
	if result == nil {
		return nil
	}
	return c.eval(result)
}

// A Builtin is a value representing a native function call.
type Builtin struct {
	// TODO:  make these values for better type checking.
	Params []Kind
	Result Kind
	Func   func(c *OpContext, args []Value) Expr

	Package Feature
	Name    string
	// REMOVE: for legacy
	Const string
}

func (x *Builtin) WriteName(w io.Writer, c *OpContext) {
	_, _ = fmt.Fprintf(w, "%s.%s", x.Package.StringValue(c), x.Name)
}

// Kind here represents the case where Builtin is used as a Validator.
func (x *Builtin) Kind() Kind {
	if len(x.Params) == 0 {
		return BottomKind
	}
	return x.Params[0]
}

func (x *Builtin) validate(c *OpContext, v Value) *Bottom {
	if x.Result != BoolKind {
		return c.NewErrf(
			"invalid validator %s: not a bool return", x.Name)
	}
	if len(x.Params) != 1 {
		return c.NewErrf(
			"invalid validator %s: may only have one validator to be used without call", x.Name)
	}
	return validateWithBuiltin(c, nil, x, []Value{v})
}

func bottom(v Value) *Bottom {
	if x, ok := v.(*Vertex); ok {
		v = x.Value
	}
	b, _ := v.(*Bottom)
	return b
}

func (x *Builtin) call(c *OpContext, call *ast.CallExpr, args []Value) Expr {
	if len(x.Params)-1 == len(args) && x.Result == BoolKind {
		// We have a custom builtin
		return &BuiltinValidator{call, x, args}
	}
	switch {
	case len(x.Params) < len(args):
		c.addErrf(0, call.Rparen,
			"too many arguments in call to %s (have %d, want %d)",
			call.Fun, len(args), len(x.Params))
		return nil
	case len(x.Params) > len(args):
		c.addErrf(0, call.Rparen,
			"not enough arguments in call to %s (have %d, want %d)",
			call.Fun, len(args), len(x.Params))
		return nil
	}
	for i, a := range args {
		if x.Params[i] != BottomKind {
			if b := bottom(a); b != nil {
				return b
			}
			if k := kind(a); x.Params[i]&k == BottomKind {
				code := EvalError
				b, _ := args[i].(*Bottom)
				if b != nil {
					code = b.Code
				}
				c.addErrf(code, pos(a),
					"cannot use %s (type %s) as %s in argument %d to %s",
					a, k, x.Params[i], i+1, call.Fun)
				return nil
			}
		}
	}
	return x.Func(c, args)
}

func (x *Builtin) Source() ast.Node { return nil }

// A BuiltinValidator is a Value that results from evaluation a partial call
// to a builtin (using CallExpr).
//
//    strings.MinRunes(4)
//
type BuiltinValidator struct {
	Src     *ast.CallExpr
	Builtin *Builtin
	Args    []Value // any but the first value
}

func (x *BuiltinValidator) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *BuiltinValidator) Kind() Kind {
	return x.Builtin.Params[0]
}

func (x *BuiltinValidator) validate(c *OpContext, v Value) *Bottom {
	args := make([]Value, len(x.Args)+1)
	args[0] = v
	copy(args[1:], x.Args)
	return validateWithBuiltin(c, x.Src, x.Builtin, args)
}

func validateWithBuiltin(c *OpContext, src *ast.CallExpr, b *Builtin, args []Value) *Bottom {
	res := b.call(c, src, args)
	switch v := res.(type) {
	case nil:
		return nil

	case *Bottom:
		return v

	case *Bool:
		if v.B {
			return nil
		}

	default:
		return c.NewErrf("invalid validator %s.%s", b.Package.StringValue(c), b.Name)
	}

	// failed:
	var buf bytes.Buffer
	b.WriteName(&buf, c)
	if len(args) > 1 {
		buf.WriteString("(")
		for i, a := range args[1:] {
			if i > 0 {
				_, _ = buf.WriteString(", ")
			}
			buf.WriteString(c.Str(a))
		}
		buf.WriteString(")")
	}
	return c.NewErrf("invalid value %s (does not satisfy %s)", c.Str(args[0]), buf.String())
}

// A Disjunction represents a disjunction, where each disjunct may or may not
// be marked as a default.
type DisjunctionExpr struct {
	Src    *ast.BinaryExpr
	Values []Disjunct

	HasDefaults bool
}

// A Disjunct is used in Disjunction.
type Disjunct struct {
	Val     Expr
	Default bool
}

func (x *DisjunctionExpr) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *DisjunctionExpr) evaluate(c *OpContext) Value {
	e := c.Env(0)
	v := &Vertex{Conjuncts: []Conjunct{{e, x, 0}}}
	c.Unifier.Unify(c, v, Finalized) // TODO: also partial okay?
	// TODO: if the disjunction result originated from a literal value, we may
	// consider the result closed to create more permanent errors.
	return v
}

// A Conjunction is a conjunction of values that cannot be represented as a
// single value. It is the result of unification.
type Conjunction struct {
	Src    ast.Expr
	Values []Value
}

func (x *Conjunction) Source() ast.Node { return x.Src }
func (x *Conjunction) Kind() Kind {
	k := TopKind
	for _, v := range x.Values {
		k &= v.Kind()
	}
	return k
}

// A disjunction is a disjunction of values. It is the result of expanding
// a DisjunctionExpr if the expression cannot be represented as a single value.
type Disjunction struct {
	Src ast.Expr

	// Values are the non-error disjuncts of this expression. The first
	// NumDefault values are default values.
	Values []*Vertex

	Errors *Bottom // []bottom

	// NumDefaults indicates the number of default values.
	NumDefaults int
}

func (x *Disjunction) Source() ast.Node { return x.Src }
func (x *Disjunction) Kind() Kind {
	k := BottomKind
	for _, v := range x.Values {
		k |= v.Kind()
	}
	return k
}

// A ForClause represents a for clause of a comprehension. It can be used
// as a struct or list element.
//
//    for k, v in src {}
//
type ForClause struct {
	Syntax *ast.ForClause
	Key    Feature
	Value  Feature
	Src    Expr
	Dst    Yielder
}

func (x *ForClause) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Syntax
}

func (x *ForClause) yield(c *OpContext, f YieldFunc) {
	n := c.node(x.Src, Finalized)
	for _, a := range n.Arcs {
		if !a.Label.IsRegular() {
			continue
		}

		n := &Vertex{status: Finalized}

		// TODO: only needed if value label != _
		b := *a
		b.Label = x.Value
		n.Arcs = append(n.Arcs, &b)

		if x.Key != 0 {
			v := &Vertex{Label: x.Key}
			key := a.Label.ToValue(c)
			v.AddConjunct(MakeRootConjunct(c.Env(0), key))
			v.SetValue(c, Finalized, key)
			n.Arcs = append(n.Arcs, v)
		}

		x.Dst.yield(c.spawn(n), f)
		if c.HasErr() {
			break
		}
	}
}

// An IfClause represents an if clause of a comprehension. It can be used
// as a struct or list element.
//
//    if cond {}
//
type IfClause struct {
	Src       *ast.IfClause
	Condition Expr
	Dst       Yielder
}

func (x *IfClause) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *IfClause) yield(ctx *OpContext, f YieldFunc) {
	if ctx.BoolValue(ctx.value(x.Condition)) {
		x.Dst.yield(ctx, f)
	}
}

// An LetClause represents a let clause in a comprehension.
//
//    for k, v in src {}
//
type LetClause struct {
	Src   *ast.LetClause
	Label Feature
	Expr  Expr
	Dst   Yielder
}

func (x *LetClause) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *LetClause) yield(c *OpContext, f YieldFunc) {
	n := &Vertex{Arcs: []*Vertex{
		{Label: x.Label, Conjuncts: []Conjunct{{c.Env(0), x.Expr, 0}}},
	}}
	x.Dst.yield(c.spawn(n), f)
}

// A ValueClause represents the value part of a comprehension.
type ValueClause struct {
	*StructLit
}

func (x *ValueClause) Source() ast.Node {
	if x.StructLit == nil {
		return nil
	}
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *ValueClause) yield(op *OpContext, f YieldFunc) {
	f(op.Env(0), x.StructLit)
}
