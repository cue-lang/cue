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
	"math/big"
	"slices"
	"strconv"
	"strings"

	"github.com/cockroachdb/apd/v3"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
)

var _ Elem = &ConjunctGroup{}

// A ConjunctGroup is an Elem that is used for internal grouping of Conjuncts
// only.
type ConjunctGroup []Conjunct

func (g *ConjunctGroup) Source() ast.Node {
	return nil
}

// A StructLit represents an unevaluated struct literal or file body.
type StructLit struct {
	Src   ast.Node // ast.File or ast.StructLit
	Decls []Decl

	// TODO: record the merge order somewhere.
}

func (o *StructLit) IsFile() bool {
	_, ok := o.Src.(*ast.File)
	return ok
}

func (x *StructLit) Source() ast.Node { return x.Src }

func (x *StructLit) evaluate(c *OpContext, state Flags) Value {
	e := c.Env(0)
	v := c.newInlineVertex(e.DerefVertex(c), nil, Conjunct{e, x, c.ci})
	// evaluate may not finalize a field, as the resulting value may be
	// used in a context where more conjuncts are added. It may also lead
	// to disjuncts being in a partially expanded state, leading to
	// misaligned nodeContexts.

	// TODO(evalv3): to be fully compatible correct, we should not always
	// finalize the arcs here. This is a temporary fix. For now, we have to do
	// this as we need a mechanism to set the arcTypeKnown bit without
	// finalizing the arcs, as they may depend on the completion of sub fields.
	// See, for instance:
	//
	// 		chainSuccess: a: {
	// 			raises?: {}
	// 			if raises == _|_ {
	// 				ret: a: 1
	// 			}
	// 			ret?: {}
	// 			if ret != _|_ {
	// 				foo: a: 1
	// 			}
	// 		}
	//
	// This would also require changing the arcType process in ForClause.yield.
	//
	// v.completeArcs(c, state)

	v.CompleteArcsShallow(c)
	return v
}

// FIELDS
//
// Fields can also be used as expressions whereby the value field is the
// expression this allows retaining more context.

// Field represents a regular field or field constraint with a fixed label.
// The label can be a regular field, definition or hidden field.
//
//	foo: bar
//	#foo: bar
//	_foo: bar
type Field struct {
	Src *ast.Field

	ArcType ArcType
	Label   Feature
	Value   Expr
}

func (x *Field) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

// A LetField represents a field that is only visible in the local scope.
//
//	let X = expr
type LetField struct {
	Src   *ast.LetClause
	Label Feature
	// IsMulti is true when this let field should be replicated for each
	// incarnation. This is the case when its expression refers to the
	// variables of a for comprehension embedded within a struct.
	IsMulti bool
	Value   Expr
}

func (x *LetField) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

// A BulkOptionalField represents a set of optional field.
//
//	[expr]: expr
type BulkOptionalField struct {
	Src    *ast.Field // Ellipsis or Field
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
//	...T
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
//	"\(expr)": expr
//	(expr): expr
type DynamicField struct {
	Src *ast.Field

	ArcType ArcType
	Key     Expr
	Value   Expr
}

func (x *DynamicField) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

// A ListLit represents an unevaluated list literal.
//
//	[a, for x in src { ... }, b, ...T]
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

func (x *ListLit) evaluate(c *OpContext, state Flags) Value {
	e := c.Env(0)
	// Pass conditions but at least set fieldSetKnown.
	v := c.newInlineVertex(e.DerefVertex(c), nil, Conjunct{e, x, c.ci})
	v.CompleteArcsShallow(c)

	// TODO(evalv3): evaluating more aggressively yields some improvements, but
	// breaks other tests. Consider using this approach, though.
	// mode := state.runMode()
	// if mode == finalize {
	// 	v.completeArcs(c, allKnown)
	// } else {
	// 	v.completeArcs(c, fieldSetKnown)
	// }
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

// BigInt sets z to the integer value of x and returns it, allocating a new
// [big.Int] if z is nil. x must be of a kind which includes [IntKind].
//
// The coefficient alone is not the value: a decimal such as 1E+2 or 80E-1 is
// an integer whose exponent scales its coefficient. Rounding to an integral
// value brings the exponent to zero, and drops nothing but zeros from an
// integer.
func (x *Num) BigInt(z *big.Int) *big.Int {
	if z == nil {
		z = &big.Int{}
	}
	var d apd.Decimal
	_, _ = internal.BaseContext.RoundToIntegralValue(&d, &x.X)
	z.Set(d.Coeff.MathBigInt())
	if d.Negative {
		z.Neg(z)
	}
	return z
}

// Int64 returns the value of x as an int64, reporting whether it fits.
// x must be of a kind which includes [IntKind].
func (x *Num) Int64() (int64, bool) {
	if x.X.Exponent == 0 {
		// Fast path for the common case, avoiding a [big.Int].
		if !x.X.Coeff.IsInt64() {
			return 0, false
		}
		i := x.X.Coeff.Int64()
		if x.X.Negative {
			i = -i
		}
		return i, true
	}
	i := x.BigInt(nil)
	if !i.IsInt64() {
		return 0, false
	}
	return i.Int64(), true
}

// Uint64 returns the value of x as a uint64, reporting whether it fits.
// x must be of a kind which includes [IntKind].
func (x *Num) Uint64() (uint64, bool) {
	if x.X.Negative {
		return 0, false
	}
	if x.X.Exponent == 0 {
		// Fast path for the common case, avoiding a [big.Int].
		if !x.X.Coeff.IsUint64() {
			return 0, false
		}
		return x.X.Coeff.Uint64(), true
	}
	i := x.BigInt(nil)
	if !i.IsUint64() {
		return 0, false
	}
	return i.Uint64(), true
}

// String is a string value. It can be used as a Value and Expr.
type String struct {
	Src ast.Node
	Str string
}

func (x *String) Source() ast.Node { return x.Src }
func (x *String) Kind() Kind       { return StringKind }

// Bytes is a bytes value. It can be used as a Value and Expr.
type Bytes struct {
	Src ast.Node
	B   []byte
}

func (x *Bytes) Source() ast.Node { return x.Src }
func (x *Bytes) Kind() Kind       { return BytesKind }

// Composites: the evaluated fields of a composite are recorded in the arc
// vertices.

type ListMarker struct {
	Src    ast.Expr
	IsOpen bool
}

func (x *ListMarker) Source() ast.Node { return x.Src }
func (x *ListMarker) Kind() Kind       { return ListKind }
func (x *ListMarker) node()            {}

type StructMarker struct {
	// TODO: once we introduce open by default lists,
	// we can get rid of StructMarker and ListMarker
	// in its entirety in favor of using type bit masks.
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
//	string
//	int
//	num
//	bool
type BasicType struct {
	Src ast.Node
	K   Kind
}

func (x *BasicType) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}
func (x *BasicType) Kind() Kind { return x.K }

// TODO: should we use UnaryExpr for Bound now we have BoundValue?

// BoundExpr represents an unresolved unary comparator.
//
//	<a
//	=~MyPattern
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

func (x *BoundExpr) evaluate(ctx *OpContext, state Flags) Value {
	// scalarKnown is used here to ensure we know the value. The result does
	// not have to be concrete, though.
	v := ctx.value(x.Expr, Flags{
		status:    partial,
		condition: scalarKnown,
		mode:      yield,
	})
	if isError(v) {
		return v
	}

	switch k := v.Kind(); k {
	case IntKind, FloatKind, NumberKind, StringKind, BytesKind:
	case NullKind, StructKind, ListKind, BoolKind:
		if x.Op != NotEqualOp && x.Op != EqualOp {
			err := ctx.NewPosf(Pos(x.Expr),
				"cannot use %s for bound %s", k, x.Op)
			return &Bottom{
				Err:  err,
				Node: ctx.vertex,
			}
		}
	default:
		mask := IntKind | FloatKind | NumberKind | StringKind | BytesKind
		if x.Op == NotEqualOp || x.Op == EqualOp {
			mask |= NullKind | StructKind | ListKind | BoolKind
		}
		if k&mask != 0 {
			ctx.addErrf(IncompleteError, token.NoPos, // TODO(errors): use ctx.pos()?
				"non-concrete value %s for bound %s", x.Expr, x.Op)
			return nil
		}
		err := ctx.NewPosf(Pos(x.Expr),
			"invalid value %s (type %s) for bound %s", v, k, x.Op)
		return &Bottom{
			Err:  err,
			Node: ctx.vertex,
		}
	}

	if v, ok := x.Expr.(Value); ok {
		if v == nil || v.Concreteness() > Concrete {
			return ctx.NewErrf("bound has fixed non-concrete value")
		}
		return &BoundValue{x.Src, x.Op, v}
	}

	if v.Concreteness() > Concrete {
		// TODO(errors): analyze dependencies of x.Expr to get positions.
		ctx.addErrf(IncompleteError, token.NoPos, // TODO(errors): use ctx.pos()?
			"non-concrete value %s for bound %s", x.Expr, x.Op)
		return nil
	}
	return &BoundValue{x.Src, x.Op, v}
}

// A BoundValue is a fully evaluated unary comparator that can be used to
// validate other values.
//
//	<5
//	=~"Name$"
type BoundValue struct {
	Src   ast.Expr
	Op    Op
	Value Value
}

func (x *BoundValue) Source() ast.Node { return x.Src }
func (x *BoundValue) Kind() Kind {
	k := x.Value.Kind()
	switch k {
	case IntKind, FloatKind, NumberKind:
		return NumberKind

	case NullKind:
		if x.Op == NotEqualOp {
			return TopKind &^ NullKind
		}
	}
	return k
}

func (x *BoundValue) validate(c *OpContext, y Value) *Bottom {
	a := y // Can be list or struct.
	b := x.Value
	if c.HasErr() {
		return c.Err()
	}

	switch v := BinOp(c, x, x.Op, a, b).(type) {
	case *Bottom:
		return v

	case *Bool:
		if v.B {
			return nil
		}
		// TODO(errors): use "invalid value %v (not an %s)" if x is a
		// predeclared identifier such as `int`.
		err := c.Newf("invalid value %v (out of bound %s)", y, x)
		err.AddPosition(y)
		return &Bottom{
			Src:  c.src,
			Err:  err,
			Code: EvalError,
			Node: c.vertex,
		}

	default:
		panic(fmt.Sprintf("unsupported type %T", v))
	}
}

func (x *BoundValue) validateStr(c *OpContext, a string) bool {
	if str, ok := x.Value.(*String); ok {
		b := str.Str
		switch x.Op {
		case LessEqualOp:
			return a <= b
		case LessThanOp:
			return a < b
		case GreaterEqualOp:
			return a >= b
		case GreaterThanOp:
			return a > b
		case EqualOp:
			return a == b
		case NotEqualOp:
			return a != b
		case MatchOp:
			return c.regexp(x.Value).MatchString(a)
		case NotMatchOp:
			return !c.regexp(x.Value).MatchString(a)
		}
	}
	return x.validate(c, &String{Str: a}) == nil
}

func (x *BoundValue) validateInt(c *OpContext, a int64) bool {
	switch n := x.Value.(type) {
	case *Num:
		b, err := n.X.Int64()
		if err != nil {
			break
		}
		switch x.Op {
		case LessEqualOp:
			return a <= b
		case LessThanOp:
			return a < b
		case GreaterEqualOp:
			return a >= b
		case GreaterThanOp:
			return a > b
		case EqualOp:
			return a == b
		case NotEqualOp:
			return a != b
		}
	}
	return x.validate(c, c.NewInt64(a)) == nil
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
func (x *NodeLink) Source() ast.Node { return x.Node.Source() }

func (x *NodeLink) resolve(c *OpContext, state Flags) *Vertex {
	return x.Node
}

// A FieldReference represents a lexical reference to a field.
//
//	a
type FieldReference struct {
	Src      *ast.Ident
	UpCount  int32
	Label    Feature
	Optional bool // true if this is a ?-marked reference (e.g., a?)
}

func (x *FieldReference) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *FieldReference) resolve(c *OpContext, state Flags) *Vertex {
	n := c.relNode(x.UpCount)
	pos := Pos(x)

	savedErrs := c.errs
	c.errs = nil
	defer func() {
		c.errs = CombineErrors(c.src, c.errs, savedErrs)
	}()

	v := c.lookup(n, pos, x.Label, state)
	if v == nil && x.Optional {
		v = c.retryOptionalLookup(n, pos, x.Label, state)
	}
	return v
}

// retryOptionalLookup handles a ?-marked lookup of l in base that failed. If
// the field is absent, the enclosing try body is marked to be discarded,
// unless the field exists in the struct the body is inserted into (see
// [OpContext.lookupInTryTarget]).
func (c *OpContext) retryOptionalLookup(base *Vertex, pos token.Pos, l Feature, flags Flags) *Vertex {
	if c.errs == nil || !c.errs.IsIncomplete() {
		return nil
	}

	savedErrs := c.errs
	if arc := c.lookupInTryTarget(base, pos, l, flags); arc != nil {
		c.errs = nil
		return arc
	}
	c.errs = savedErrs

	c.markSkipTry()
	return nil
}

// lookupInTryTarget retries a ?-marked lookup of l in base, which failed
// during the pre-evaluation of a try body, against the struct the body will
// be inserted into. See [TryClause.yield].
//
// The pre-evaluation runs in an inline vertex, so a field the body declares
// shadows the same-named field of the enclosing struct: in
//
//	x: a: 1
//	try { x: b: x.a? }
//
// the reference x.a resolves to the body's x, which has no a, whereas the
// real evaluation unifies the two and finds a. To decide existence, follow
// the path from the body root down to base in the vertex the body is inserted
// into and look up l there. This recurses for enclosing try bodies, as that
// vertex is itself an inline vertex for a nested body.
//
// Only bases reached through the body's own fields are covered. A base bound
// by a let or for clause, or structure-shared with a rooted vertex, is not a
// descendant of the body root, and the lookup then fails as before.
func (c *OpContext) lookupInTryTarget(base *Vertex, pos token.Pos, l Feature, flags Flags) *Vertex {
	root := base.tryBodyRoot()
	if root == nil {
		return nil
	}
	target := root.state.tryBody.target
	if target == nil {
		return nil
	}

	var path []Feature
	for v := base; v != root; v = v.Parent {
		path = append(path, v.Label)
	}
	c.errs = nil
	for i := len(path) - 1; i >= 0; i-- {
		if target = c.lookup(target, pos, path[i], flags); target == nil {
			return nil
		}
	}
	if arc := c.lookup(target, pos, l, flags); arc != nil {
		return arc
	}
	return c.lookupInTryTarget(target, pos, l, flags)
}

// markSkipTry records that a ?-marked reference failed to resolve because its
// optional field is not present. The failure is attributed to the nearest
// enclosing try clause body by walking up the parent chain from the vertex
// currently being evaluated until it reaches a body whose [nodeContext.tryBody]
// is set (see [TryClause.yield]).
//
// A single shared flag cannot distinguish which try a failed reference belongs
// to when try bodies nest or their finalizations interleave: an inner body's
// finalization may be in progress on the call stack while a sibling reference
// belonging to an outer body fails (e.g. `foo? & { try {...} }`). The vertex
// owning the failing reference, tracked by the scheduler in c.vertex, is not
// affected by this interleaving, so resolving the owner by structural ancestry
// rather than by stack order attributes the failure correctly (issue #4347).
func (c *OpContext) markSkipTry() {
	if root := c.vertex.tryBodyRoot(); root != nil {
		root.state.tryBody.skip = true
	}
}

// A ValueReference represents a lexical reference to a value.
//
// Example: an X referring to
//
//	a: X=b
type ValueReference struct {
	Src     *ast.Ident
	UpCount int32
	Label   Feature // for informative purposes.
}

func (x *ValueReference) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *ValueReference) resolve(c *OpContext, state Flags) *Vertex {
	if x.UpCount == 0 {
		return c.vertex
	}
	// A standalone expression evaluated without a scope has no enclosing
	// struct for self to refer to.
	e := c.Env(x.UpCount - 1)
	if e.DerefVertex(c) == nil {
		c.addErrf(0, Pos(x), "self has no enclosing struct")
		return nil
	}
	return c.derefNode(e)
}

// A LabelReference refers to the string or integer value of a label.
//
// Example: an X referring to
//
//	[X=Pattern]: b: a
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

func (x *LabelReference) evaluate(ctx *OpContext, state Flags) Value {
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

// A DynamicReference is like a FieldReference, but with a computed label.
//
// Example: an X referring to
//
//	X=(x): v
//	X="\(x)": v
//	X=[string]: v
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

func (x *DynamicReference) EvaluateLabel(ctx *OpContext, env *Environment) Feature {
	env = env.up(ctx, x.UpCount)
	frame := ctx.PushState(env, x.Src)
	v := ctx.value(x.Label, Flags{
		status:    partial,
		condition: scalarKnown,
		mode:      yield,
	})
	ctx.PopState(frame)
	return ctx.Label(x, v)
}

func (x *DynamicReference) resolve(ctx *OpContext, state Flags) *Vertex {
	e := ctx.Env(x.UpCount)
	frame := ctx.PushState(e, x.Src)
	v := ctx.value(x.Label, Flags{
		status:    partial,
		condition: scalarKnown,
		mode:      yield,
	})
	ctx.PopState(frame)
	f := ctx.Label(x.Label, v)
	return ctx.lookup(e.DerefVertex(ctx), Pos(x), f, state)
}

// An ImportReference refers to an imported package.
//
// Example: strings in
//
//	import "strings"
//
//	strings.ToLower("Upper")
type ImportReference struct {
	Src        *ast.Ident
	ImportPath Feature
	// Instance holds the build instance that the import
	// resolves to. This is nil for standard library imports.
	Instance *build.Instance
	Label    Feature // for informative purposes
}

func (x *ImportReference) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *ImportReference) resolve(ctx *OpContext, state Flags) *Vertex {
	var v *Vertex
	if x.Instance != nil {
		v = ctx.Runtime.LoadInstance(x.Instance)
		// Resolve to a per-evaluation instance rather than the runtime's
		// shared cached root, which would race when evaluated in place.
		v = ctx.importInstance(v)
	} else {
		v = ctx.Runtime.LoadBuiltin(x.ImportPath.StringValue(ctx))
	}
	if v == nil {
		ctx.addErrf(EvalError, x.Src.Pos(), "cannot find package %q",
			x.ImportPath.StringValue(ctx))
	}
	return v
}

// importInstance returns a private, per-evaluation instance of an imported
// package root. The runtime caches one shared, immutable compiled template per
// package; evaluating it in place would race across goroutines, so each
// evaluation gets its own instance sharing only the template's conjuncts.
//
// No environment rewriting is needed: a package root's conjuncts bind
// references to the vertex being evaluated (see [nodeContext.scheduleStruct]),
// not to a pointer stored in the template, so a fresh root reusing those
// conjuncts resolves its internal references against itself.
func (c *OpContext) importInstance(template *Vertex) *Vertex {
	if template == nil {
		return nil
	}
	if v, ok := c.importInstances[template]; ok {
		return v
	}
	v := &Vertex{
		Parent:             template.Parent,
		importTemplate:     template,
		Label:              template.Label,
		ClosedRecursive:    template.ClosedRecursive,
		ClosedNonRecursive: template.ClosedNonRecursive,
		HasEllipsis:        template.HasEllipsis,
		ArcType:            template.ArcType,
		// Clip so appends during evaluation do not write into the shared
		// template's backing array.
		Conjuncts: slices.Clip(template.Conjuncts),
	}
	// A failed-to-compile instance is cached as a bottom vertex with no
	// conjuncts; carry the error over.
	if len(template.Conjuncts) == 0 {
		if b, ok := template.BaseValue.(*Bottom); ok {
			v.BaseValue = b
			v.status = finalized
		}
	}
	if c.importInstances == nil {
		c.importInstances = map[*Vertex]*Vertex{}
	}
	c.importInstances[template] = v
	return v
}

// A LetReference evaluates a let expression in its original environment.
//
// Example: an X referring to
//
//	let X = x
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

func (x *LetReference) resolve(ctx *OpContext, state Flags) *Vertex {
	e := ctx.Env(x.UpCount)

	// A let bound to a bare value reference, like `let X = self`, denotes
	// the identity of an enclosing vertex, not a computed value. Resolve it
	// as ValueReference.resolve would: a pure environment lookup, which can
	// never be incomplete. Other let expressions need the synthetic vertex
	// below as a caching and suspension point, but evaluating a self
	// binding there would unify the enclosing vertex into its own child.
	// For rooted vertices structure sharing masks this; for dynamic
	// (inline) vertices sharing is disabled and the synthetic vertex
	// becomes a lazily populated mirror of its own parent, rebinding self
	// to the mirror and misresolving nested references through the let.
	if vr, ok := x.X.(*ValueReference); ok && vr.UpCount > 0 {
		return ctx.derefNode(e.up(ctx, vr.UpCount-1))
	}

	// No need to Unify n, as Let references can only result from evaluating
	// an expression within n, in which case evaluation must already have
	// started.

	arc := ctx.lookup(e.DerefVertex(ctx), Pos(x), x.Label, state)
	if arc == nil {
		return nil
	}

	// Using a let arc directly saves an allocation, but should not be done
	// in the following circumstances:
	// 1) multiple Environments to be resolved for a single let
	// 2) in case of error: some errors, like structural cycles, may only
	//    occur when an arc is resolved directly, but not when used in an
	//    expression. Consider, for instance:
	//
	//        a: {
	//            b: 1
	//            let X = a  // structural cycle
	//            c: X.b     // not a structural cycle
	//        }
	//
	//     In other words, a Vertex is not necessarily erroneous when a let
	//     field contained in that Vertex is erroneous.

	// NOTE: eval v2 used to finalize here.
	// We should only partly finalize the result here as it is not safe to
	// finalize any references made by the let.

	b := arc.Bottom()
	// Check if the arc is currently being evaluated to prevent infinite
	// recursion when a let references itself through a field selector.
	// If the arc has a running state, we must use the cache mechanism
	// to properly detect and handle cycles.
	arcState := arc.getState(ctx)
	if !arc.MultiLet && ((b == nil && arcState == nil) || isCyclePlaceholder(b)) {
		return arc
	}

	// Not caching let expressions may lead to exponential behavior.
	// The expr uses the expression of a Let field, which can never be used in
	// any other context.
	c := arc.ConjunctAt(0)
	expr := c.Expr()

	// A let field always has a single expression and thus ConjunctGroups
	// should always have been eliminated. This is critical, as we must
	// ensure that Comprehensions, which may be wrapped in ConjunctGroups,
	// are eliminated.
	_, isGroup := expr.(*ConjunctGroup)
	ctx.Assertf(Pos(expr), !isGroup, "unexpected number of expressions")

	// TODO(mem): add counter for let cache usage.
	key := cacheKey{expr, arc}
	entry, ok := e.cache[key]
	if ok {
		// A cached cycle placeholder is provisional: it was computed before a
		// dependency had settled. Recompute it in case the dependency now has a
		// concrete value, but only while the let arc has not finalized: once it
		// has, the value is settled and recomputing would loop on the same
		// placeholder forever.
		//
		// Only the original computation may be discarded this way. A recompute
		// that again results in a placeholder gained nothing, and evaluating
		// the recomputed entry can itself re-run resolve for the same key (for
		// example when a try body referencing the let is dry-run as part of
		// the evaluation), so discarding recomputed entries as well spawns
		// fresh computations without bound.
		if isCyclePlaceholder(entry.v.BaseValue) && arc.status != finalized &&
			!entry.fromRecompute {
			ok = false
		}
	}
	if !ok {
		// Link in the right environment to ensure comprehension context is not
		// lost. Use a Vertex to piggyback on cycle processing.
		c.Env = e
		c.x = expr

		if e.cache == nil {
			e.cache = map[cacheKey]letCacheEntry{}
		}
		// Allocate a vertex with space for one conjunct.
		var alloc struct {
			v Vertex
			c [1]Conjunct
		}
		alloc.c[0] = c
		alloc.v = Vertex{
			Parent:    arc.Parent,
			Label:     x.Label,
			IsDynamic: b != nil && b.Code == StructuralCycleError,
			Conjuncts: alloc.c[:],
		}
		n := &alloc.v
		// A non-nil entry.v means this computation replaces a discarded
		// placeholder.
		entry = letCacheEntry{v: n, fromRecompute: entry.v != nil}
		e.cache[key] = entry
		// TODO(mem): enable again once we implement memory management.
		// nc := n.getState(ctx)
		// TODO: unlike with the old evaluator, we do not allow the first
		// cycle to be skipped. Doing so can lead to hanging evaluation.
		// As the cycle detection works slightly differently in the new
		// evaluator (and is not entirely completed), this can happen. We
		// should revisit this once we have completed the structural cycle
		// detection.
		// nc.hasNonCycle = true
		// Allow a first cycle to be skipped.
		// nc.free()

		// Parents cannot add more conjuncts to a let expression, so set of
		// conjuncts is always complete.
		//
		// NOTE(let finalization): as this let expression is not recorded as
		// a subfield within its parent arc, setParentDone will not be called
		// as part of normal processing. The same is true for finalization.
		// The use of setParentDone has the additional effect that the arc
		// will be finalized where it is needed. See the namesake NOTE for the
		// location where this is triggered.
		n.setParentDone()
	}
	return entry.v
}

// A SelectorExpr looks up a fixed field in an expression.
//
//	a.sel
//	a.sel? (optional - returns OptionalUndefined if field doesn't exist)
type SelectorExpr struct {
	Src      *ast.SelectorExpr
	X        Expr
	Sel      Feature
	Optional bool // true if selector has ? suffix (e.g., foo.bar?)
}

func (x *SelectorExpr) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *SelectorExpr) resolve(c *OpContext, state Flags) *Vertex {
	n := c.node(x, x.X, x.Sel.IsRegular(), Flags{
		status:    partial,
		condition: needFieldSetKnown,
		mode:      yield,
	})
	if n == emptyNode {
		return n
	}
	// TODO(eval): dynamic nodes should be fully evaluated here as the result
	// will otherwise be discarded and there will be no other chance to check
	// the struct is valid.

	savedErrs := c.errs
	c.errs = nil
	defer func() {
		c.errs = CombineErrors(c.src, c.errs, savedErrs)
	}()

	pos := x.Src.Sel.Pos()
	v := c.lookup(n, pos, x.Sel, state)
	if v == nil && x.Optional {
		v = c.retryOptionalLookup(n, pos, x.Sel, state)
	}
	return v
}

// IndexExpr is like a selector, but selects an index.
//
//	a[index]
//	a[index]? (optional - returns OptionalUndefined if index doesn't exist)
type IndexExpr struct {
	Src      *ast.IndexExpr
	X        Expr
	Index    Expr
	Optional bool // true if index has ? suffix (e.g., foo[0]?)
}

func (x *IndexExpr) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *IndexExpr) resolve(ctx *OpContext, state Flags) *Vertex {
	// TODO: support byte index.
	n := ctx.node(x, x.X, true, Flags{
		status:    partial,
		condition: needFieldSetKnown,
		mode:      yield,
	})
	i := ctx.value(x.Index, Flags{
		status:    partial,
		condition: scalarKnown,
		mode:      yield,
	})
	if n == emptyNode {
		return n
	}
	// TODO(eval): dynamic nodes should be fully evaluated here as the result
	// will otherwise be discarded and there will be no other chance to check
	// the struct is valid.

	f := ctx.Label(x.Index, i)

	// Within lookup, errors collected in ctx may be associated with n. This is
	// correct if the error is generated within lookup, but not if it has
	// already been generated at this point. We therefore bail out early here if
	// we already have an error.
	// TODO: this code can probably go once we have cleaned up error generation.
	if ctx.errs != nil {
		return nil
	}

	// TODO: uncomment once above code can be removed.
	// savedErrs := ctx.errs
	// ctx.errs = nil
	// defer func() {
	// 	ctx.errs = CombineErrors(ctx.src, ctx.errs, savedErrs)
	// }()

	pos := x.Src.Index.Pos()
	v := ctx.lookup(n, pos, f, state)
	if v == nil && x.Optional {
		v = ctx.retryOptionalLookup(n, pos, f, state)
	}
	return v
}

// A SliceExpr represents a slice operation. (Not currently in spec.)
//
//	X[Lo:Hi:Stride]
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

func (x *SliceExpr) evaluate(c *OpContext, state Flags) Value {
	// TODO: strides

	v := c.value(x.X, Flags{
		status:    partial,
		condition: fieldSetKnown,
		mode:      yield,
	})
	const as = "slice index"

	switch v := v.(type) {
	case nil:
		c.addErrf(IncompleteError, c.pos(), "non-concrete slice subject %s", x.X)
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
			lo = c.uint64(c.value(x.Lo, Flags{
				status:    partial,
				condition: scalarKnown,
				mode:      yield,
			}), as)
		}
		if x.Hi != nil {
			hi = c.uint64(c.value(x.Hi, Flags{
				status:    partial,
				condition: scalarKnown,
				mode:      yield,
			}), as)
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
				c.AddBottom(&Bottom{
					Src:  a.Source(),
					Err:  err,
					Node: v,
				})
				return nil
			}
			if v.IsDynamic {
				// If the list is dynamic, there is no need to recompute the
				// arcs.
				a.Label = label
				n.Arcs = append(n.Arcs, a)
				continue
			}
			arc := *a
			arc.Parent = n
			arc.Label = label
			n.Arcs = append(n.Arcs, &arc)
		}
		n.status = finalized
		return n

	case *Bytes:
		var (
			lo = uint64(0)
			hi = uint64(len(v.B))
		)
		if x.Lo != nil {
			lo = c.uint64(c.value(x.Lo, Flags{
				status:    partial,
				condition: scalarKnown,
				mode:      yield,
			}), as)
		}
		if x.Hi != nil {
			hi = c.uint64(c.value(x.Hi, Flags{
				status:    partial,
				condition: scalarKnown,
				mode:      yield,
			}), as)
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
	return c.NewErrf("cannot slice %v (type %s)", v, v.Kind())
}

// An Interpolation is a string interpolation.
//
//	"a \(b) c"
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

func (x *Interpolation) evaluate(c *OpContext, state Flags) Value {
	var sb strings.Builder
	for _, e := range x.Parts {
		v := c.value(e, Flags{
			status:    partial,
			condition: scalarKnown,
			mode:      yield,
		})
		if x.K == BytesKind {
			sb.Write(c.ToBytes(v))
		} else {
			sb.WriteString(c.ToString(v))
		}
	}
	if err := c.Err(); err != nil {
		err = &Bottom{
			Code: err.Code,
			Node: c.vertex,
			Err:  errors.Wrapf(err.Err, Pos(x), "invalid interpolation"),
		}
		// c.AddBottom(err)
		// return nil
		return err
	}
	if x.K == BytesKind {
		// Interpolations in bytes literals are not very common;
		// it's okay that we allocate twice in this case.
		return &Bytes{x.Src, []byte(sb.String())}
	}
	return &String{x.Src, sb.String()}
}

// UnaryExpr is a unary expression.
//
//	Op X
//	-X !X +X
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

func (x *UnaryExpr) evaluate(c *OpContext, state Flags) Value {
	if !c.concreteIsPossible(x.Op, x.X) {
		return nil
	}
	v := c.value(x.X, Flags{
		status:    partial,
		condition: scalarKnown,
		mode:      yield,
	})
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
		expectedKind = NumberKind

	case AddOp:
		if v, ok := v.(*Num); ok {
			// TODO: wrap in thunk to save position of '+'?
			return v
		}
		expectedKind = NumberKind

	case NotOp:
		if v, ok := v.(*Bool); ok {
			return c.NewBool(!v.B)
		}
		expectedKind = BoolKind
	}
	if k&expectedKind != BottomKind {
		c.addErrf(IncompleteError, Pos(x.X),
			"operand %s of '%s' not concrete (was %s)", x.X, op, k)
		return nil
	}
	return c.NewErrf("invalid operation %v (%s %s)", x, op, k)
}

// BinaryExpr is a binary expression.
//
//	X + Y
//	X & Y
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

func (x *BinaryExpr) evaluate(c *OpContext, state Flags) Value {
	env := c.Env(0)
	if x.Op == AndOp {
		v := c.newInlineVertex(nil, nil, makeAnonymousConjunct(env, x, c.ci.Refs))

		// Do not fully evaluate the Vertex: if it is embedded within a
		// struct with arcs that are referenced from within this expression,
		// it will end up adding "locked" fields, resulting in an error.
		// It will be the responsibility of the "caller" to get the result
		// to the required state. If the struct is already dynamic, we will
		// evaluate the struct regardless to ensure that cycle reporting
		// keeps working.
		envVertex := env.DerefVertex(c)
		if (c.inDetached == 0 && envVertex != nil && envVertex.IsDynamic) || c.inValidator > 0 {
			v.Finalize(c)
		} else {
			v.CompleteArcsShallow(c)
		}

		return v
	}

	// Short-circuit evaluation for && and ||: when the left operand alone
	// determines the result, skip evaluating the right operand. This matches
	// the spec ("The right operand is evaluated conditionally") and suppresses
	// any error or incomplete value on the right when not needed.
	if Pos(x).Experiment().ShortCircuit {
		switch x.Op {
		case BoolAndOp:
			if c.concreteIsPossible(x.Op, x.X) {
				left, _ := c.concrete(env, x.X, x.Op)
				if b, ok := left.(*Bool); ok && !b.B {
					return c.NewBool(false)
				}
			}
		case BoolOrOp:
			if c.concreteIsPossible(x.Op, x.X) {
				left, _ := c.concrete(env, x.X, x.Op)
				if b, ok := left.(*Bool); ok && b.B {
					return c.NewBool(true)
				}
			}
		}
	}

	if !c.concreteIsPossible(x.Op, x.X) || !c.concreteIsPossible(x.Op, x.Y) {
		return nil
	}

	// TODO: allow comparing to a literal Bottom only. Find something more
	// principled perhaps. One should especially take care that two values
	// evaluating to Bottom don't evaluate to true. For now we check for
	// Bottom here and require that one of the values be a Bottom literal.
	if x.Op == EqualOp || x.Op == NotEqualOp {
		if isLiteralBottom(x.X) {
			return c.validate(env, x.Src, x.Y, x.Op, state)
		}
		if isLiteralBottom(x.Y) {
			return c.validate(env, x.Src, x.X, x.Op, state)
		}
	}

	left, _ := c.concrete(env, x.X, x.Op)
	right, _ := c.concrete(env, x.Y, x.Op)

	if err := CombineErrors(x.Src, left, right); err != nil {
		return err
	}

	if err := c.Err(); err != nil {
		return err
	}

	return BinOp(c, x, x.Op, left, right)
}

// OpenExpr represents the ... operator to disable typo checking.
//
// #A...
type OpenExpr struct {
	Src *ast.PostfixExpr
	X   Expr
}

func (x *OpenExpr) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *OpenExpr) evaluate(c *OpContext, state Flags) Value {
	c.ci.Opened = true
	return c.evalState(x.X, state)
}

// A Function represents an unevaluated CUE function literal.
type Function struct {
	Src    *ast.Func
	Params []FuncParam
	Ret    Expr
	Body   Expr

	// Open marks a bodyless partial signature: the parameter list ends in
	// "..." and may be extended by unification with other signatures. A
	// function with an implementation body is always closed.
	Open bool
}

// FuncParam represents a compiled function parameter.
type FuncParam struct {
	Src        *ast.FuncParam
	Label      Feature
	Local      Feature
	Value      Expr
	Positional bool

	// Default is the parameter's declared default expression, or nil. It is
	// compiled in the closure scope, like Value. A call may leave a parameter
	// unbound only if it is optional or has a default; an unbound default is
	// used as the parameter's argument, and a supplied argument makes the
	// default irrelevant to the call.
	Default Expr

	// ArcType records the parameter's requiredness: ArcMember for a plain
	// parameter, ArcRequired for p!, and ArcOptional for p?.
	ArcType ArcType
}

// A FuncValue is a function closure captured in an evaluation environment.
//
// Types holds the function types (bodyless signatures) the value has been
// unified with; their parameter constraints and result constraints are
// additionally enforced on every call (see [nodeContext.scheduleFuncCall]),
// A plain name from an attached signature supplies the contract label of a
// matched positional slot when that slot was otherwise unnamed.
// For a bodyless FuncValue — itself a function type — Types holds the other
// types it has been met with.
type FuncValue struct {
	Src   *ast.Func
	Fn    *Function
	Env   *Environment
	Types []FuncType

	// args holds arguments bound by partial application, indexed by parameter;
	// a nil expr marks an unbound parameter. When any entry is set this is a
	// partially applied function: calling it binds the remaining parameters
	// and, once none are unbound, evaluates the body. Each argument retains
	// the environment it was bound in, which may differ from the environment
	// of a later completing call.
	args []funcArg
}

// A funcArg is an argument bound to a function parameter by a partial
// application, together with the environment in which it resolves.
type funcArg struct {
	expr Expr
	env  *Environment
}

func (x *Function) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *Function) evaluate(c *OpContext, state Flags) Value {
	env := c.Env(0)
	if b := x.scheduleDefaultCheck(c, env); b != nil {
		return b
	}
	return &FuncValue{
		Src: x.Src,
		Fn:  x,
		Env: env,
	}
}

// hasDefaults reports whether any parameter of x declares a default.
func (x *Function) hasDefaults() bool {
	for i := range x.Params {
		if x.Params[i].Default != nil {
			return true
		}
	}
	return false
}

// A funcDefaultCheck records a function literal whose declared parameter
// defaults are to be checked once the enclosing vertex has completed; see
// [Function.scheduleDefaultCheck]. holder is the vertex whose conjunct the
// literal was evaluated for; it receives the error, so that the function
// value itself is in error.
type funcDefaultCheck struct {
	fn     *Function
	env    *Environment
	holder *Vertex
}

// scheduleDefaultCheck arranges for the literal's declared defaults to be
// checked against their constraints (see [Function.checkDefaults]), so that
// an inconsistent default is reported whether or not the function is ever
// called. The check is deferred until the enclosing vertex has completed
// evaluating all of its arcs: a default may refer to values that are still
// in flight, including the very field that holds the literal, and finalizing
// such a reference would freeze it as cyclic. When the enclosing vertex has
// already completed, nothing it holds is in flight and the check runs now,
// making the literal itself the error.
func (x *Function) scheduleDefaultCheck(c *OpContext, env *Environment) *Bottom {
	if !x.hasDefaults() {
		return nil
	}
	if v := env.DerefVertex(c); v != nil {
		if n := v.state; n != nil && v.status < finalized {
			n.funcDefaultChecks = append(n.funcDefaultChecks, funcDefaultCheck{
				fn:     x,
				env:    env,
				holder: c.vertex,
			})
			return nil
		}
	}
	return x.checkDefaults(c, env)
}

// checkDefaults checks each declared parameter default against its
// parameter's constraint, evaluated in the closure environment, where a call
// evaluates them too. The check runs once per literal and closure. A default
// that is not resolvable — an incomplete value, or a cycle, as for a default
// that calls its own function — leaves the check undone: the call that omits
// the parameter reports the incompleteness or the cycle. A default that is
// itself an error, or that conflicts with the constraint, is reported at the
// default's position.
func (x *Function) checkDefaults(c *OpContext, env *Environment) *Bottom {
	key := funcAnchorKey{fn: x, env: env}
	if c.checkingDefaults[key] || c.funcDefaultsChecked[key] {
		return nil
	}
	if c.checkingDefaults == nil {
		c.checkingDefaults = map[funcAnchorKey]bool{}
		c.funcDefaultsChecked = map[funcAnchorKey]bool{}
	}
	c.checkingDefaults[key] = true
	defer delete(c.checkingDefaults, key)
	c.funcDefaultsChecked[key] = true

	for i := range x.Params {
		p := &x.Params[i]
		if p.Default == nil || p.Value == nil {
			continue
		}
		if b := c.checkFuncParamDefault(env, i, p); b != nil {
			return b
		}
	}
	return nil
}

// uncheckableDefault reports whether b leaves a default check undone rather
// than failed: an incomplete value or a cycle.
func uncheckableDefault(b *Bottom) bool {
	return b == nil || b.IsIncomplete() || b.Code == CycleError || b.Code == StructuralCycleError
}

// invalidDefault wraps the error b of a parameter's default as an error at
// the default's position, retaining b's own positions.
func (c *OpContext) invalidDefault(i int, p *FuncParam, b *Bottom) *Bottom {
	name := funcParamName(c, i, p)
	src := p.Default.Source()
	var at token.Pos
	if src != nil {
		at = src.Pos()
	}
	return &Bottom{
		Src:  src,
		Code: b.Code,
		Err:  errors.Wrapf(b.Err, at, "invalid default for parameter %s", name),
	}
}

// checkFuncParamDefault unifies a parameter's default with its constraint,
// as a call omitting the parameter would, and reports a conflict, or an
// error in the default itself, at the default's position. Incomplete results
// and cycles leave the check undone.
func (c *OpContext) checkFuncParamDefault(env *Environment, i int, p *FuncParam) *Bottom {
	savedErrs := c.errs
	c.errs = nil
	defer func() { c.errs = savedErrs }()

	s := c.PushState(env, p.Default.Source())
	defer c.PopState(s)

	// Evaluate the default on its own first: an error in the default itself
	// is reported as such, and an unresolvable default leaves the check
	// undone without consulting the constraint.
	flags := Flags{status: partial, mode: yield}
	def := c.evalState(p.Default, flags)
	b := bottom(def)
	if b == nil && def == nil {
		return nil
	}
	if b == nil {
		b = c.errs
	}
	if b != nil {
		if uncheckableDefault(b) {
			return nil
		}
		return c.invalidDefault(i, p, b)
	}
	con := c.evalState(p.Value, flags)
	if b := bottom(con); b != nil || con == nil || c.errs != nil {
		return nil
	}

	v := c.newInlineVertex(nil, nil,
		MakeConjunct(env, p.Default, c.ci),
		MakeConjunct(env, p.Value, c.ci))
	v.Finalize(c)

	b = v.Bottom()
	if b == nil {
		b = c.errs
	}
	if uncheckableDefault(b) {
		return nil
	}
	return c.invalidDefault(i, p, b)
}

// funcParamName names a parameter in a diagnostic: its callable label, else
// its body-local name, else its 1-based position.
func funcParamName(c *OpContext, i int, p *FuncParam) string {
	switch {
	case p.Label != InvalidLabel:
		return p.Label.SelectorString(c)
	case p.Local != InvalidLabel:
		return p.Local.SelectorString(c)
	}
	return strconv.Itoa(i + 1)
}

// hasDefault reports whether parameter i of x has a declared default: its own,
// or one declared by an attached signature for the parameter it matched. A
// defaulted parameter may be left unbound by a call.
func (x *FuncValue) hasDefault(i int) bool {
	return FuncParamHasDefault(x.Fn, x.Types, i)
}

// FuncParamHasDefault reports whether parameter i of fn declares a default,
// itself or through one of the signatures in types attached to it. Such a
// parameter is omittable: a call may leave it unbound, and it is admitted
// beyond a closed signature like an optional one.
func FuncParamHasDefault(fn *Function, types []FuncType, i int) bool {
	if fn.Params[i].Default != nil {
		return true
	}
	for _, t := range types {
		matches := matchFuncParamsToValue(t.Fn, fn, types)
		for j, tp := range t.Fn.Params {
			if tp.Default != nil && matches[j] == i {
				return true
			}
		}
	}
	return false
}

// FuncParamArcType returns the requiredness of parameter i of fn under the
// signatures in types attached to it: the strictest marking among fn's own
// parameter and the parameters of types that match it. As for struct fields,
// a plain parameter is stricter than a required one, which is stricter than
// an optional one, so a parameter is optional only if every signature that
// declares it marks it optional.
func FuncParamArcType(fn *Function, types []FuncType, i int) ArcType {
	a := fn.Params[i].ArcType
	for _, t := range types {
		matches := matchFuncParamsToValue(t.Fn, fn, types)
		for j, tp := range t.Fn.Params {
			if matches[j] == i && tp.ArcType < a {
				a = tp.ArcType
			}
		}
	}
	return a
}

// composedParam reports whether parameter i of x is declared by every
// function composed with x. A composed function only admits the parameters
// all its functions declare, as a closed type admits only its own.
func composedParam(x *FuncValue, i int) bool {
	for _, t := range x.Types {
		if t.Fn.Body != nil && !slices.Contains(matchFuncParamsToValue(t.Fn, x.Fn, x.Types), i) {
			return false
		}
	}
	return true
}

// funcArgBound reports whether an activation arc holds an argument. The
// conjuncts scheduleFuncCall adds to an arc are exactly the constraint and
// default expressions of the function's own signature and of the signatures
// attached to it, so any other conjunct is the argument a call bound to the
// parameter. An arc without an argument belongs to a parameter the call left
// unbound. (The IsFuncArg mark of an argument conjunct is not used here: it
// is inherited by the conjuncts of a call made as an argument of another.)
func funcArgBound(a *Vertex, fn *Function, types []FuncType) bool {
	for _, c := range a.Conjuncts {
		if !isFuncSignatureExpr(c.x, fn, types) {
			return true
		}
	}
	return false
}

// isFuncSignatureExpr reports whether x is a constraint or default expression
// of fn or of one of the signatures in types.
func isFuncSignatureExpr(x Node, fn *Function, types []FuncType) bool {
	for i := range fn.Params {
		p := &fn.Params[i]
		if (p.Value != nil && x == p.Value) || (p.Default != nil && x == p.Default) {
			return true
		}
	}
	for _, t := range types {
		for i := range t.Fn.Params {
			p := &t.Fn.Params[i]
			if (p.Value != nil && x == p.Value) || (p.Default != nil && x == p.Default) {
				return true
			}
		}
	}
	return false
}

func (x *FuncValue) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *FuncValue) Kind() Kind { return FuncKind }

// scheduleFuncCall schedules the payload of a function call once its call
// reference has resolved to the function's anchor vertex and passed cycle
// detection. env is the call's body environment: env.Vertex holds the
// activation (one arc per parameter, seeded with the argument conjuncts) and
// env.Up is the closure environment.
//
// The body is scheduled under env, so its parameter references resolve
// against the activation, exactly as they resolved against the parameter
// fields of the former template struct: the activation stands in for the one
// struct level assumed by the compiled UpCounts. Parameter constraints and
// the return type are compiled in the closure scope and are therefore pinned
// to env.Up, preserving the documented scoping (e.g. `b: a` refers to an
// outer `a`, not the sibling parameter).
//
// The constraint and body conjuncts carry ci, which by now includes the
// anchor's cycle reference, so recursive calls in either the body or a
// parameter constraint (e.g. `f: func(a: f(1)) -> int: 1`) participate in
// structural cycle detection. The argument conjuncts, in contrast, were
// created with the caller's CloseInfo and never inherit the anchor's cycle
// references: an argument that calls the same function (twice(twice(2))) is
// nesting, not recursion.
func (n *nodeContext) scheduleFuncCall(ref *FuncCallRef, env *Environment, ci CloseInfo) {
	fn := ref.fn
	if act := env.Vertex; act != nil {
		// The activation arcs are built in parameter order (see
		// [FuncValue.call]), so walk them in lockstep with the parameters
		// instead of rescanning the arc list for every parameter.
		k := 0
		for i, p := range fn.Params {
			if p.Value == nil && p.Default == nil {
				continue
			}
			local := p.Local
			if local == InvalidLabel {
				// Anonymous parameters bind their argument to a synthetic
				// activation arc; see [anonParamLabel].
				local = anonParamLabel(n.ctx, i)
			}
			for k < len(act.Arcs) && act.Arcs[k].Label != local {
				k++
			}
			if k < len(act.Arcs) {
				a := act.Arcs[k]
				// A parameter the call left unbound takes its declared
				// default as the argument. Like the constraint, the default
				// is compiled in the closure scope and carries the anchored
				// ci, so a default that calls the function itself is a
				// structural cycle rather than unbounded recursion. A bound
				// parameter never sees its default.
				if p.Default != nil && !funcArgBound(a, fn, ref.types) {
					a.addConjunctUnchecked(MakeConjunct(env.Up, p.Default, ci))
				}
				if p.Value != nil {
					a.addConjunctUnchecked(MakeConjunct(env.Up, p.Value, ci))
				}
			}
		}
	}
	// The function types the called value was unified with contribute their
	// parameter constraints and result constraints as well. A type parameter
	// constrains the value parameter it statically matched: positional slots
	// align by ordinal and name-only parameters match by label (see
	// [checkFuncTightening]). Like the value's own constraints,
	// the type's constraints are compiled in the type's closure scope and are
	// therefore pinned to the type's environment, with the same anchored ci.
	//
	// A type's parameters do not follow the activation's arc order, so for
	// them the arcs are indexed by label first.
	var argArcs map[Feature]*Vertex
	if act := env.Vertex; act != nil && len(ref.types) > 0 {
		argArcs = make(map[Feature]*Vertex, len(act.Arcs))
		for _, a := range act.Arcs {
			argArcs[a.Label] = a
		}
	}
	for _, t := range ref.types {
		if argArcs != nil {
			matches := matchFuncParamsToValue(t.Fn, fn, ref.types)
			for i, tp := range t.Fn.Params {
				if tp.Value == nil && tp.Default == nil {
					continue
				}
				vi := matches[i]
				if vi < 0 {
					// An unmatched optional type parameter has no value
					// parameter, so there is no activation arc to constrain.
					continue
				}
				vp := fn.Params[vi]
				local := vp.Local
				if local == InvalidLabel {
					// Anonymous value parameters bind their argument to a
					// synthetic activation arc; see [anonParamLabel].
					local = anonParamLabel(n.ctx, vi)
				}
				if a := argArcs[local]; a != nil {
					// A default declared by the type carries over to an
					// unbound parameter; when the value declares one too,
					// the two unify in the arc.
					if tp.Default != nil && !funcArgBound(a, fn, ref.types) {
						a.addConjunctUnchecked(MakeConjunct(t.Env, tp.Default, ci))
					}
					if tp.Value != nil {
						a.addConjunctUnchecked(MakeConjunct(t.Env, tp.Value, ci))
					}
				}
			}
		}
		if t.Fn.Ret != nil {
			n.scheduleConjunct(MakeConjunct(t.Env, t.Fn.Ret, ci), ci)
		}
	}
	// A signature with an implementation was composed with the called value:
	// its body is evaluated as well and unifies with the result. It gets its
	// own activation, binding its own local names, whose arcs hold the same
	// conjuncts as the arcs of the parameters they matched, so that all
	// implementations see the same arguments, constraints, and defaults.
	for _, t := range ref.types {
		if t.Fn.Body != nil {
			n.scheduleComposedBody(t, fn, ref.types, argArcs, ci)
		}
	}
	n.scheduleConjunct(MakeConjunct(env, fn.Body, ci), ci)
	if fn.Ret != nil {
		n.scheduleConjunct(MakeConjunct(env.Up, fn.Ret, ci), ci)
	}
}

// scheduleComposedBody schedules the body of t, a function composed with the
// called function fn, in an activation derived from argArcs, the activation
// arcs of fn indexed by label. A parameter of t that matched no parameter of
// fn is omittable (see [mergeFuncValues]) and is never bound by a call: it
// takes its default, if any, or is absent.
func (n *nodeContext) scheduleComposedBody(t FuncType, fn *Function, types []FuncType, argArcs map[Feature]*Vertex, ci CloseInfo) {
	act := n.ctx.newInlineVertex(nil, nil)
	matches := matchFuncParamsToValue(t.Fn, fn, types)
	for i, p := range t.Fn.Params {
		local := p.Local
		if local == InvalidLabel {
			local = anonParamLabel(n.ctx, i)
		}
		arc := &Vertex{
			Parent:    act,
			Label:     local,
			ArcType:   ArcMember,
			IsDynamic: true,
		}
		var src *Vertex
		if vi := matches[i]; vi >= 0 {
			vlocal := fn.Params[vi].Local
			if vlocal == InvalidLabel {
				vlocal = anonParamLabel(n.ctx, vi)
			}
			src = argArcs[vlocal]
		}
		switch {
		case src != nil:
			arc.ArcType = src.ArcType
			arc.Conjuncts = slices.Clone(src.Conjuncts)
		case p.Default != nil:
			arc.addConjunctUnchecked(MakeConjunct(t.Env, p.Default, ci))
			if p.Value != nil {
				arc.addConjunctUnchecked(MakeConjunct(t.Env, p.Value, ci))
			}
		case p.Value != nil:
			arc.ArcType = ArcOptional
			arc.addConjunctUnchecked(MakeConjunct(t.Env, p.Value, ci))
		default:
			continue
		}
		act.Arcs = append(act.Arcs, arc)
	}
	env := &Environment{Up: t.Env, Vertex: act}
	n.scheduleConjunct(MakeConjunct(env, t.Fn.Body, ci), ci)
}

// funcAnchorKey identifies the cached anchor vertex for a function literal
// captured in a particular closure environment.
type funcAnchorKey struct {
	fn  *Function
	env *Environment
}

// funcCallRefKey identifies the cached call reference for a function call
// site captured in a particular closure environment.
type funcCallRefKey struct {
	call *CallExpr
	env  *Environment
}

// funcCallResultKey identifies the memo bucket of a call site: the AST call
// node and the caller environment in which the call's arguments resolve.
// Environment pointers may be shared across disjunct branches (overlay
// cloning copies task environments), so the same key may be dispatched to
// different callees; the entries within a bucket are therefore additionally
// matched on callee identity. See [OpContext.funcCallResults].
type funcCallResultKey struct {
	call *CallExpr
	env  *Environment
}

// A funcCallResult memoizes the finalized result of a completed function
// call together with the identity of the callee that produced it. The callee
// is matched the way [equalTerminal] compares function values: the function
// literal, its closure environment, its recorded type constraints, and the
// arguments bound by partial application.
type funcCallResult struct {
	fn     *Function
	env    *Environment
	types  []FuncType
	args   []funcArg
	result *Vertex
}

// matches reports whether a memoized result was produced by the given
// callee.
func (r *funcCallResult) matches(c *OpContext, x *FuncValue) bool {
	return r.fn == x.Fn &&
		(r.env == x.Env || r.env.Equal(c, x.Env)) &&
		equalFuncTypes(r.types, x.Types) &&
		equalFuncArgs(r.args, x.args)
}

// A FuncCallRef is a stable reference to a function's anchor vertex, carrying
// the function whose call it schedules. Unlike [NodeLink] it is a pure
// [Resolver] (not also a [Value]), so it is scheduled through handleResolver
// and routes through [nodeContext.detectCycle]; the payload of the call is
// then dispatched by [nodeContext.scheduleFuncCall]. Reusing the same
// FuncCallRef across (recursive) calls is what lets the detector recognize
// re-entry as a structural cycle, while distinct call sites reaching the same
// anchor are treated as ordinary nesting.
type FuncCallRef struct {
	src    ast.Node
	fn     *Function
	types  []FuncType // extra signature constraints of the called value
	target *Vertex
}

func (x *FuncCallRef) Source() ast.Node { return x.src }

// Func returns the function whose call this reference schedules. It allows
// traversals outside this package, such as feature marking in export, to
// descend into the function's expressions.
func (x *FuncCallRef) Func() *Function { return x.fn }

// Types returns the extra signature constraints of the called value. Their
// parameter and result constraints are scheduled alongside the function's
// own (see [nodeContext.scheduleFuncCall]), so, like the function returned
// by [FuncCallRef.Func], their expressions are reachable through the call
// and must be visited by traversals outside this package.
func (x *FuncCallRef) Types() []FuncType { return x.types }

func (x *FuncCallRef) resolve(c *OpContext, state Flags) *Vertex {
	return x.target
}

// unresolvedDisjunction returns the disjunction underlying v if v does not
// resolve to a single value, following the same traversal as
// [OpContext.getDefault]. It returns nil if v resolves (getDefault succeeds)
// or is not a disjunction.
func unresolvedDisjunction(v Value) *Disjunction {
	_, d := resolveDefault(v)
	return d
}

func (x *FuncValue) call(c *OpContext, call *CallExpr, state Flags) Value {
	if x.Fn == nil || x.Fn.Body == nil {
		c.AddErrf("cannot call function without implementation")
		return nil
	}

	callEnv := c.Env(0)

	// A call's result is fully determined by its call site, the caller
	// environment in which its arguments resolve, and the callee. If this
	// combination has already produced a finalized, error-free result, reuse
	// it: a parameter referenced N times, or a call nested as an argument and
	// re-scheduled once per use of the enclosing parameter, would otherwise
	// rebuild the activation and re-finalize the body every time, making
	// deeply nested calls exponential in their nesting depth. Only completed
	// error-free results are memoized (see the write below), so a call
	// re-entered through recursion — which the structural cycle detector must
	// observe on the shared anchor — has no entry and is never
	// short-circuited here.
	resultKey := funcCallResultKey{call: call, env: callEnv}
	for i := range c.funcCallResults[resultKey] {
		if r := &c.funcCallResults[resultKey][i]; r.matches(c, x) {
			return r.result
		}
	}

	// Phase 1: bind arguments to parameters. bindings[i] is the argument bound
	// to parameter i — by an earlier partial application (x.args) or by this
	// call — with the environment it resolves in; a nil expr marks an unbound
	// parameter. Labeled arguments resolve through the value's declared
	// contract labels, including one supplied by an attached signature for an
	// otherwise unnamed positional slot.
	bindings := make([]funcArg, len(x.Fn.Params))
	copy(bindings, x.args)
	used := make([]bool, len(call.Args))
	byLabel, _ := funcParamLabels(x.Fn, x.Types)

	nextPos := 0
	for i, p := range x.Fn.Params {
		if bindings[i].expr != nil {
			continue // already bound by an earlier partial application
		}
		var arg Expr

		if p.Positional {
			for nextPos < len(call.Args) && nextPos < len(call.ArgLabels) && call.ArgLabels[nextPos] != InvalidLabel {
				nextPos++
			}
			if nextPos < len(call.Args) && (nextPos >= len(call.ArgLabels) || call.ArgLabels[nextPos] == InvalidLabel) {
				arg = call.Args[nextPos]
				used[nextPos] = true
				nextPos++
			}
		}

		var boundLabel Feature
		for j, label := range call.ArgLabels {
			param, ok := byLabel[label]
			if !ok || param != i {
				continue
			}
			switch {
			case boundLabel != InvalidLabel:
				c.AddErrf("duplicate argument %s", label.SelectorString(c))
				return nil
			case arg != nil:
				c.AddErrf("argument %s provided by position and label", label.SelectorString(c))
				return nil
			}
			arg = call.Args[j]
			used[j] = true
			boundLabel = label
		}

		if arg != nil && !composedParam(x, i) {
			// A function composed with x declares no such parameter, so no
			// call can bind it, whichever of the functions is the head.
			if boundLabel != InvalidLabel {
				c.AddErrf("unknown argument %s", boundLabel.SelectorString(c))
			} else {
				c.AddErrf("too many positional arguments in function call")
			}
			return nil
		}
		if arg != nil {
			bindings[i] = funcArg{expr: arg, env: callEnv}
		}
	}

	// reportUnusedArg reports an argument that bound no parameter. A missing
	// required or non-defaulted argument (checked below in phase 2) takes
	// precedence for a completing call, so this runs after that check there,
	// but before returning a partial application.
	reportUnusedArg := func() bool {
		for i := range call.Args {
			if !used[i] {
				if i < len(call.ArgLabels) && call.ArgLabels[i] != InvalidLabel {
					c.AddErrf("unknown argument %s", call.ArgLabels[i].SelectorString(c))
				} else {
					c.AddErrf("too many positional arguments in function call")
				}
				return true
			}
		}
		return false
	}

	// A partial application binds the given arguments and yields a function
	// over the remaining parameters, rather than evaluating the body. The
	// bound arguments are carried on the returned value and combined with a
	// later call's arguments when the function is called to completion.
	if call.Partial {
		if reportUnusedArg() {
			return nil
		}
		return &FuncValue{Src: x.Src, Fn: x.Fn, Env: x.Env, Types: x.Types, args: bindings}
	}

	// Phase 2: complete the call.
	//
	// anchor is the stable vertex representing this function literal. Every
	// call re-references it, so the regular structural cycle detector
	// observes recursive re-entry the same way it does for the hand-written
	// `(f & {...}).out` pattern. It carries no conjuncts: the payload of a
	// call travels on the reference and is dispatched by scheduleFuncCall.
	//
	// ref is the reference used to reach the anchor. It is stable per call
	// site (this *CallExpr), not merely per function: the detector treats a
	// re-occurring arc reached by the SAME reference (r.Ref == x) as a cycle,
	// but a different reference (r.Ref != x) as ordinary nesting. Distinct
	// call sites therefore nest freely (e.g. twice(twice(2))), while a single
	// call site re-entered through recursion is flagged as a structural
	// cycle. This mirrors `(f & {n: (f & {...}).out}).out`, where the two `f`
	// references are distinct AST nodes.
	anchor := c.funcAnchor(x.Fn, x.Env)
	ref := c.funcCallRef(call, anchor, x)

	arcs := make([]*Vertex, 0, len(x.Fn.Params))
	for i, p := range x.Fn.Params {
		arg := bindings[i].expr
		argEnv := bindings[i].env

		// A parameter may be left unbound only if it is optional or has a
		// declared default; a required parameter (p!) is the name-only
		// counterpart of a plain one and takes a default the same way.
		// Requiredness combines across the attached signatures as for struct
		// fields, so a parameter is optional only if every signature marks it
		// so. Whatever its constraint evaluates to plays no part in this
		// decision: a default reached through a reference is an ordinary
		// part of the constraint. An unbound default is added to the
		// parameter's arc by scheduleFuncCall.
		arcType := FuncParamArcType(x.Fn, x.Types, i)
		if arg == nil && arcType != ArcOptional && !x.hasDefault(i) {
			switch {
			case arcType == ArcRequired:
				c.AddErrf("missing required argument %s", p.Label.SelectorString(c))
			case p.Label != InvalidLabel:
				c.AddErrf("missing argument %s", p.Label.SelectorString(c))
			default:
				c.AddErrf("not enough arguments in function call")
			}
			return nil
		}
		local := p.Local
		if local == InvalidLabel {
			// Anonymous positional parameters are not referenceable from the
			// function body, but their arguments are still checked against
			// the parameter constraint and any type constraints matched to
			// the parameter: the argument is bound to a synthetic activation
			// arc that scheduleFuncCall addresses by the parameter's index.
			local = anonParamLabel(c, i)
		}
		if arg == nil && p.Value == nil && !x.hasDefault(i) {
			// Nothing binds this parameter: no argument was provided and
			// there is neither a constraint nor a default.
			continue
		}

		// The activation carries one arc per parameter. The argument is
		// evaluated in the environment it was bound in — the caller's, or an
		// earlier partial application's — mirroring the original
		// `(f & {param: arg})` binding. The parameter constraint is appended
		// by scheduleFuncCall with the anchored CloseInfo once the call
		// reference passes cycle detection.
		arc := &Vertex{
			Label:     local,
			ArcType:   ArcMember,
			IsDynamic: true,
		}
		if arg == nil && p.ArcType == ArcOptional && !x.hasDefault(i) {
			// An omitted optional parameter is an optional field of the
			// activation, holding only its constraint: a reference to it
			// from the body reports the error a reference to an absent
			// optional field reports, rather than seeing the bare
			// constraint.
			arc.ArcType = ArcOptional
		}
		if arg != nil {
			// IsFuncArg pins the caller's cycle-reference chain to the
			// argument, so that its (lazy) evaluation never adopts the
			// callee body's chain; see [CycleInfo.IsFuncArg].
			ci := c.ci
			ci.IsFuncArg = true
			arc.Conjuncts = ConjunctGroup{MakeConjunct(argEnv, arg, ci)}
		}
		arcs = append(arcs, arc)
	}

	if reportUnusedArg() {
		return nil
	}

	// Assemble the per-call activation and evaluate the call as a fresh
	// inline vertex holding only the call reference. The vertex is
	// non-rooted, so a recursive call that re-reaches the anchor through ref
	// while this call is in progress is flagged as a structural cycle. The
	// reference's payload — body, return type, and parameter constraints —
	// is dispatched by scheduleFuncCall once the reference passes cycle
	// detection. The result is the vertex itself: there is no result arc to
	// select, and parameters live on the separate activation, so unifying
	// two call results does not unify their arguments.
	// The activation is created through newInlineVertex so that it is
	// registered for scope-based reclamation: deferred body tasks may touch
	// it after this function returns, so its node context can only be
	// released once the enclosing evaluation completes.
	activation := c.newInlineVertex(nil, nil)
	activation.Arcs = arcs
	for _, a := range arcs {
		a.Parent = activation
	}
	bodyEnv := &Environment{Up: x.Env, Vertex: activation}
	result := c.newInlineVertex(anchor, nil, MakeConjunct(bodyEnv, ref, c.ci))
	result.Finalize(c)
	if b := result.Bottom(); b != nil && !b.IsIncomplete() {
		return b
	}

	// Force validation of parameters the body did not reference: an argument
	// conflicting with its constraint must fail the call even if the result
	// does not depend on it. An arc that finalizes to an incomplete error —
	// typically an argument referencing a field that is not yet defined —
	// makes the call itself incomplete, so that evaluation can retry once
	// more information arrives, exactly as it would for len(y.q). Note that
	// an unprovided optional or defaulted parameter does not qualify: its arc
	// holds only the (non-bottom) constraint.
	for _, a := range arcs {
		a.Finalize(c)
		if b := a.Bottom(); b != nil {
			return b
		}
	}

	// Memoize only a finalized, error-free result. An incomplete result may
	// resolve differently once more information is available on a later
	// evaluation, so it must not be cached (an incomplete argument arc
	// already returned above); a recursion cycle produces a (non-incomplete)
	// bottom above and likewise leaves no entry, which is what keeps
	// memoization from short-circuiting cycle detection.
	if result.Bottom() == nil {
		if c.funcCallResults == nil {
			c.funcCallResults = map[funcCallResultKey][]funcCallResult{}
		}
		c.funcCallResults[resultKey] = append(c.funcCallResults[resultKey],
			funcCallResult{fn: x.Fn, env: x.Env, types: x.Types, args: x.args, result: result})
	}

	return result
}

// funcAnchor returns the stable anchor vertex for the given function literal
// captured in environment env. The anchor deliberately carries no conjuncts —
// the payload of a call travels on the FuncCallRef and is dispatched by
// scheduleFuncCall — but creating it once per (literal, closure) and reusing
// it across (recursive) calls lets the structural cycle detector observe
// re-entry, because every call reaches the same arc.
func (c *OpContext) funcAnchor(fn *Function, env *Environment) *Vertex {
	if c.funcAnchors == nil {
		c.funcAnchors = map[funcAnchorKey]*Vertex{}
	}
	key := funcAnchorKey{fn: fn, env: env}
	if v := c.funcAnchors[key]; v != nil {
		return v
	}

	v := &Vertex{
		Parent:    env.DerefVertex(c),
		ArcType:   ArcMember,
		IsDynamic: true,
	}
	c.funcAnchors[key] = v
	return v
}

// funcCallRef returns the stable reference used to reach the anchor vertex
// from a particular call site. It is cached per (call site, closure) so that
// re-entry through the SAME call site (recursion) is detected as a cycle
// (r.Ref == x), while distinct call sites reaching the same anchor (nesting,
// e.g. twice(twice(2))) are treated as ordinary structure (r.Ref != x).
//
// The reference also carries the signature constraints of the called value,
// dispatched together with the call payload by scheduleFuncCall. A call site
// may in principle reach FuncValues that share a literal and closure but
// were tightened by different types, so refs are matched on those
// constraints as well; refs for the same (call site, closure) are kept in a
// small list. Recursion through a consistently tightened value reuses one
// ref, preserving cycle detection.
func (c *OpContext) funcCallRef(call *CallExpr, anchor *Vertex, x *FuncValue) *FuncCallRef {
	if c.funcCallRefs == nil {
		c.funcCallRefs = map[funcCallRefKey][]*FuncCallRef{}
	}
	key := funcCallRefKey{call: call, env: x.Env}
	for _, r := range c.funcCallRefs[key] {
		if r.target == anchor && slices.Equal(r.types, x.Types) {
			return r
		}
	}
	r := &FuncCallRef{src: call.Source(), fn: x.Fn, types: x.Types, target: anchor}
	c.funcCallRefs[key] = append(c.funcCallRefs[key], r)
	return r
}

func (c *OpContext) validate(env *Environment, src ast.Node, x Expr, op Op, flags Flags) (r Value) {
	state := flags.status

	s := c.PushState(env, src)

	match := op != EqualOp // non-error case

	c.inValidator++
	// Note that evalState may call yield, so we need to balance the counter
	// with a defer.
	defer func() { c.inValidator-- }()
	req := Flags{
		status:    state,
		condition: needTasksDone,
		mode:      finalize,
	}
	v := c.evalState(x, req)
	u, _ := c.getDefault(v)
	u = Unwrap(u)

	// If our final (unwrapped) value is potentially a recursive structure, we
	// still need to recursively check for errors. We do so by treating it
	// as the original value, which if it is a Vertex will be evaluated
	// recursively below.
	if u != nil && u.Kind().IsAnyOf(StructKind|ListKind) {
		u = v
	}

	switch v := u.(type) {
	case nil:
	case *Bottom:
		switch v.Code {
		case CycleError:
			c.PopState(s)
			c.AddBottom(v)
			// TODO: add this. This erases some
			// c.verifyNonMonotonicResult(env, x, true)
			return nil

		case IncompleteError:
			// The referenced node is actively being computed and still has
			// value-producing tasks pending; yield until valueKnown is met.
			// schedRUNNING (not schedREADY) avoids deadlocking on a task that
			// nobody else will trigger — a genuine cycle. The same yield
			// serves both ArcPending (load-bearing: the comp body will fire
			// and materialize the arc) and ArcOptional (usually a no-op, but
			// the arc may still be upgraded to a member by sibling decls
			// before valueKnown).
			if v.Node != nil {
				ns := v.Node.state
				if ns != nil && ns.state == schedRUNNING &&
					!ns.meets(valueKnown) &&
					ns.provided&valueKnown != 0 {
					if t := c.current(); t != nil {
						c.PopState(s)
						sched := &ns.scheduler
						t.waitFor(sched, valueKnown)
						sched.yield()
						panic("unreachable")
					}
				}
			}

			c.evalState(x, Flags{
				status:    finalized,
				condition: allKnown,
				mode:      ignore,
			})

			// We have a nonmonotonic use of a failure. Referenced fields should
			// not be added anymore.
			c.verifyNonMonotonicResult(env, x, true)
		}

		match = op == EqualOp

	case *Vertex:
		// TODO(cycle): if EqualOp:
		// - ensure to pass special status to if clause or keep a track of "hot"
		//   paths.
		// - evaluate hypothetical struct
		// - walk over all fields and verify that fields are not contradicting
		//   previously marked fields.
		//

		if c.hasDepthCycle(v) {
			c.verifyNonMonotonicResult(env, x, true)
			match = op == EqualOp
			break
		}

		v.Finalize(c)

		switch {
		case isFinalError(v):
			// Need to recursively check for errors, so we need to evaluate the
			// Vertex in case it hadn't been evaluated yet.
			match = op == EqualOp
		}

	default:
		if v.Kind().IsAnyOf(CompositeKind) && v.Concreteness() > Concrete && state < conjuncts {
			c.PopState(s)
			c.AddBottom(cycle)
			return nil
		}

		c.verifyNonMonotonicResult(env, x, false)

		if v.Concreteness() > Concrete {
			// TODO: mimic comparison to bottom semantics. If it is a valid
			// value, check for concreteness that this level only. This
			// should ultimately be replaced with an exists and valid
			// builtin.
			match = op == EqualOp
		}

		c.evalState(x, Flags{
			status:    state,
			condition: needTasksDone,
			mode:      yield,
		})
	}

	c.PopState(s)
	return c.NewBool(match)
}

func isFinalError(n *Vertex) bool {
	n = n.DerefValue()
	if b, ok := Unwrap(n).(*Bottom); ok && b.Code < IncompleteError {
		return true
	}
	return false
}

// verifyNonMonotonicResult re-evaluates the given expression at a later point
// to ensure that the result has not changed. This is relevant when a function
// uses reflection, as in `if a != _|_`, where the result of an evaluation may
// change after the fact.
// expectError indicates whether the value should evaluate to an error or not.
func (c *OpContext) verifyNonMonotonicResult(env *Environment, x Expr, expectError bool) {
	if n := env.DerefVertex(c).state; n != nil {
		n.postChecks = append(n.postChecks, envCheck{
			env:         env,
			ci:          c.ci,
			expr:        x,
			expectError: expectError,
		})
	}
}

// A CallExpr represents a call to a builtin.
//
//	len(x)
//	strings.ToLower(x)
type CallExpr struct {
	Src       *ast.CallExpr
	Fun       Expr
	Args      []Expr
	ArgLabels []Feature

	// Partial marks a call written with a trailing "..." (f(a: 1, ...)). Such
	// a call binds the given arguments and yields a function over the
	// remaining parameters instead of evaluating the body.
	Partial bool
}

func (x *CallExpr) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *CallExpr) hasArgLabels() bool {
	for _, label := range x.ArgLabels {
		if label != InvalidLabel {
			return true
		}
	}
	return false
}

func (x *CallExpr) evaluate(c *OpContext, state Flags) Value {
	v := c.evalState(x.Fun, Flags{
		status:    partial,
		condition: concreteKnown,
		mode:      yield,
		concrete:  true,
	})
	if d := unresolvedDisjunction(v); d != nil {
		// Calling an unresolved disjunction is ambiguous: it is not known
		// which disjunct to call. Further unification may still resolve
		// the disjunction to a single value, so this is an incomplete
		// error, consistent with other uses of unresolved disjunctions.
		c.addErrf(IncompleteError, Pos(x.Fun),
			"cannot call %s: unresolved disjunction %v (type %s)", x.Fun, d, d.Kind())
		return nil
	}
	fun, _ := c.getDefault(v)
	fun = Unwrap(fun)
	switch f := fun.(type) {
	case *Builtin:
		if x.Partial {
			c.AddErrf("partial application of builtin %s is not supported", x.Fun)
			return nil
		}
		if x.hasArgLabels() {
			// The legacy validator-constructor form leaves raw slot zero for
			// the value validated later. Its argument list is not represented
			// by an attached function type, so applying full-call labels here
			// could silently bind a constructor argument to the wrong raw slot.
			if f.IsValidator(len(x.Args)) {
				c.AddErrf("labeled arguments are not supported for validator constructor %s", x.Fun)
				return nil
			}
			// A labeled argument binds through the parameter names of
			// the builtin's declared signature; the call proceeds with
			// the arguments in positional order.
			args, ok := bindBuiltinArgs(c, f, x)
			if !ok {
				return nil
			}
			x2 := *x
			x2.Args = args
			x2.ArgLabels = nil
			return f.rawCall(c, &x2, state)
		}
		return f.rawCall(c, x, state)

	case *FuncValue:
		return f.call(c, x, state)

	case *ExternalFunc:
		return f.rawCall(c, x, state)

	case *BuiltinValidator:
		if x.hasArgLabels() {
			// A bare validator's argument is the value it validates; its
			// declared signature names at most that one parameter, so a
			// labeled call has no position left to select.
			c.AddErrf("labeled arguments are not supported for validator %s", x.Fun)
			return nil
		}
		if x.Partial {
			c.AddErrf("partial application of builtin %s is not supported", x.Fun)
			return nil
		}
		// We allow a validator that takes no arguments except the validated
		// value to be called with zero arguments.
		switch {
		case f.Src != nil:
			c.AddErrf("cannot call previously called validator %s", x.Fun)
			return nil

		case f.Builtin.IsValidator(len(x.Args)):
			v := *f
			v.Src = x
			return &v

		default:
			return f.Builtin.rawCall(c, x, state)
		}

	case *Bottom:
		// The callee is an error: propagate it instead of complaining that
		// an error is not a function.
		return f

	case nil:
		// The callee did not evaluate to a value. The cause, if any, has
		// already been reported through the context.
		if !c.HasErr() {
			c.addErrf(IncompleteError, Pos(x.Fun),
				"cannot call incomplete value %s", x.Fun)
		}
		return nil

	default:
		if !IsConcrete(fun) && fun.Kind()&FuncKind != 0 {
			c.addErrf(IncompleteError, Pos(x.Fun), "cannot call non-concrete value %s (type %s)", x.Fun, kind(fun))
		} else {
			c.AddErrf("cannot call non-function %s (type %s)", x.Fun, kind(fun))
		}
		return nil
	}
}

func (builtin *Builtin) rawCall(c *OpContext, call *CallExpr, state Flags) Value {
	callCtx := BuiltinCallContext{
		ctx:     c,
		call:    call,
		builtin: builtin,
	}
	if builtin.RawFunc != nil {
		if !builtin.checkArgs(c, Pos(call), len(call.Args)) {
			return nil
		}
		// RawFunc builtins evaluate their arguments themselves, so the
		// parameter constraints of any recorded function types are not
		// enforced for them; the result constraints are.
		return builtin.applyResultTypes(c, builtin.RawFunc(callCtx))
	}
	// Arguments to functions are open. This mostly matters for NonConcrete
	// builtins.
	saved := c.ci
	c.ci.FromDef = false
	c.ci.FromEmbed = false
	defer func() {
		c.ci.FromDef = saved.FromDef
		c.ci.FromEmbed = saved.FromEmbed
	}()

	args := make([]Value, 0, len(call.Args))
	for i, a := range call.Args {
		saved := c.errs
		c.errs = nil
		// XXX: XXX: clear id.closeContext per argument and remove from runTask?

		var expr Value
		if builtin.NonConcrete {
			expr = c.evalState(a, Flags{
				status:    state.status,
				condition: state.condition,
				mode:      state.mode,
			})
		} else {
			flags := Flags{
				status:    state.status,
				condition: state.condition | fieldSetKnown | concreteKnown | disjunctionTask,
				mode:      state.mode,
			}
			if builtin.PerDisjunct {
				expr = c.valueOrDisjunction(a, flags)
			} else {
				expr = c.value(a, flags)
			}
		}

		switch v := expr.(type) {
		case nil:
			if c.errs == nil {
				// There SHOULD be an error in the context. If not, we generate
				// one.
				c.Assertf(Pos(call.Fun), c.HasErr(),
					"argument %d to function %s is incomplete", i, call.Fun)
			}

		case *Bottom:
			// HACK / workaround: if we pass a cycle placeholder as error,
			// the function may yield _. This is incorrect, but works when the
			// value is a scalar, as the postCheck mechanism will catch and
			// reevaluate the cycle.
			// This, however, does not work for composite values. The right fix
			// would be to "freeze" the composite struct and simply see if the
			// returned value subsumes the result. For now, we simply error
			// in these cases.
			if v.IsIncomplete() && isCyclePlaceholder(v) &&
				(builtin.Result == StructKind || builtin.Result == ListKind) {
				v = &Bottom{
					Src:  v.Src,
					Err:  v.Err,
					Code: v.Code,
					Node: v.Node,
				}
			}
			// TODO(errors): consider adding an argument index for this errors.
			c.errs = CombineErrors(a.Source(), c.errs, v)

		default:
			args = append(args, expr)
		}
		c.errs = CombineErrors(a.Source(), saved, c.errs)
	}
	if c.HasErr() {
		return nil
	}
	if builtin.IsValidator(len(args)) {
		return &BuiltinValidator{
			Src:     call,
			Builtin: builtin,
			Args:    args,
			Env:     c.Env(0),
		}
	}

	// Materialize defaults before applying attached signature constraints,
	// so that an omitted defaulted argument is constrained just like an
	// explicit argument.
	args, ok := builtin.completeArgs(c, Pos(call), args, Flags{
		status:    state.status,
		condition: state.condition | fieldSetKnown | concreteKnown | disjunctionTask,
		mode:      state.mode,
	})
	if !ok {
		return nil
	}

	if builtin.PerDisjunct {
		if d := disjunctionOf(args[0]); d != nil {
			return builtin.callPerDisjunct(c, callCtx, args, d, state)
		}
	}
	return builtin.finishCall(c, callCtx, args, state)
}

// finishCall applies the attached function types to args, calls the
// builtin with them, and evaluates its result.
func (builtin *Builtin) finishCall(c *OpContext, callCtx BuiltinCallContext, args []Value, state Flags) Value {
	args, b := builtin.applyParamTypes(c, args)
	if b != nil {
		return b
	}

	callCtx.args = args
	result := builtin.call(callCtx)
	switch result := result.(type) {
	case nil:
		return nil
	case *Bottom:
		vErr := c.NewPosf(Pos(callCtx.call), "error in call to %s", builtin.qualifiedName(c))
		return &Bottom{
			Code: result.Code,
			Err:  errors.Wrap(vErr, result.Err),
			Node: c.vertex,
		}
	}
	v, ci := c.evalStateCI(result, Flags{status: partial, condition: state.condition, mode: state.mode})
	c.ci = ci
	return builtin.applyResultTypes(c, v)
}

// callPerDisjunct calls the builtin once per disjunct of d, the disjunction
// its first argument holds, and returns the disjunction of the results with
// the defaults kept, so that f(a | b) is f(a) | f(b). A disjunct whose call
// fails is eliminated, as unification would eliminate it; the errors are
// reported only when every disjunct fails.
func (builtin *Builtin) callPerDisjunct(c *OpContext, callCtx BuiltinCallContext, args []Value, d *Disjunction, state Flags) Value {
	vals := make([]Value, 0, len(d.Values))
	numDefaults := 0
	var errs *Bottom
	for i, v := range d.Values {
		a := slices.Clone(args)
		a[0] = v
		saved := c.errs
		c.errs = nil
		r := builtin.finishCall(c, callCtx, a, state)
		if r == nil && c.errs != nil {
			r = c.errs
		}
		c.errs = saved
		switch r := r.(type) {
		case nil:
			return nil
		case *Bottom:
			errs = CombineErrors(nil, errs, r)
		default:
			if i < d.NumDefaults {
				numDefaults++
			}
			vals = append(vals, r)
		}
	}
	switch len(vals) {
	case 0:
		return errs
	case 1:
		return vals[0]
	}
	return &Disjunction{
		Src:         d.Src,
		Values:      vals,
		Errors:      d.Errors,
		NumDefaults: numDefaults,
		HasDefaults: d.HasDefaults,
	}
}

// applyParamTypes applies every attached function type's parameter
// constraints to args, which are indexed by raw builtin position.
// Positionally bindable parameters map by ordinal; a name-only parameter may
// map through the contract label supplied by another attached signature.
// Like native function constraints, these expressions are
// evaluated in the environment in which their signature was declared.
func (builtin *Builtin) applyParamTypes(c *OpContext, args []Value) ([]Value, *Bottom) {
	for _, t := range builtin.Types {
		matches := matchBuiltinParams(t.Fn, builtin)
		for j, tp := range t.Fn.Params {
			i := matches[j]
			if tp.Value == nil || i < 0 || i >= len(args) || args[i] == nil {
				continue
			}
			// A constraint that only restricts the kind is already enforced
			// by the builtin's own parameter kind, which was checked against
			// the type when the two were unified. Skip it rather than
			// materialize the argument (see kindOnlyConstraint).
			if k, ok := kindOnlyConstraint(tp.Value); ok &&
				i < len(builtin.Params) && builtin.Params[i].Kind()&^k == 0 {
				continue
			}
			w := c.newInlineVertex(nil, nil,
				MakeConjunct(t.Env, tp.Value, c.ci),
				MakeConjunct(nil, args[i], c.ci))
			w.Finalize(c)
			if b := w.Bottom(); b != nil && !b.IsIncomplete() {
				return nil, b
			}
			args[i] = w
		}
	}
	return args, nil
}

// applyResultTypes unifies the result of a builtin call with the result
// constraints of the function types the builtin was unified with. The
// constraints are evaluated in their type's environment.
func (x *Builtin) applyResultTypes(c *OpContext, v Value) Value {
	if len(x.Types) == 0 || v == nil {
		return v
	}
	if _, ok := v.(*Bottom); ok {
		return v
	}
	a := make([]Conjunct, 0, len(x.Types)+1)
	a = append(a, MakeConjunct(nil, v, c.ci))
	for _, t := range x.Types {
		if t.Fn.Ret == nil {
			continue
		}
		// As for arguments: a constraint that only restricts the kind cannot
		// reject a result the builtin can produce, since its result kind was
		// checked against the type at unification (see kindOnlyConstraint).
		if k, ok := kindOnlyConstraint(t.Fn.Ret); ok && x.Result&^k == 0 {
			continue
		}
		a = append(a, MakeConjunct(t.Env, t.Fn.Ret, c.ci))
	}
	if len(a) == 1 {
		return v
	}
	w := c.newInlineVertex(nil, nil, a...)
	w.Finalize(c)
	return w
}

// A Builtin is a value representing a native function call.
type Builtin struct {
	// TODO:  make these values for better type checking.
	Params []Param
	Result Kind

	// NonConcrete should be set to true if a builtin supports non-concrete
	// arguments. By default, all arguments are checked to be concrete.
	NonConcrete bool

	// PerDisjunct marks a builtin which applies to each disjunct of an
	// unresolved disjunction argument, so that f(a | b) is f(a) | f(b).
	// Its Func receives such an argument as a [Disjunction], defaults and
	// all, where other builtins report it as ambiguous.
	PerDisjunct bool

	Func func(call BuiltinCallContext) Expr

	// RawFunc gives low-level control to CUE's internals for builtins.
	// It should be used when fine control over the evaluation process is
	// needed. Note that RawFuncs are responsible for returning a Value. This
	// gives them fine control over how exactly such value gets evaluated.
	// A RawFunc may pass CycleInfo, errors and other information through
	// the Context.
	//
	// TODO: consider merging Func and RawFunc into a single field again.
	RawFunc func(call BuiltinCallContext) Value

	// Added indicates as of which language version this builtin can be used.
	Added string

	Package Feature
	Name    string

	// Types holds the function types (bodyless signatures) this builtin has
	// been unified with; their parameter constraints and result constraints
	// are additionally enforced on every ordinary full call (see
	// [Builtin.rawCall]). The legacy validator-constructor path remains
	// separate. A
	// Builtin carrying Types is a clone of a package-level builtin, which
	// remains identified through orig.
	Types []FuncType

	// orig points to the package-level builtin from which a tightened clone
	// (a builtin carrying Types) was derived. It is nil for an original
	// builtin. See [Builtin.self].
	orig *Builtin
}

// self returns the identity of the builtin: the package-level builtin from
// which a tightened clone was derived, or the builtin itself.
func (x *Builtin) self() *Builtin {
	if x.orig != nil {
		return x.orig
	}
	return x
}

type Param struct {
	Value Value
}

// Kind returns the kind mask of this parameter.
func (p Param) Kind() Kind {
	return p.Value.Kind()
}

// Default reports the default value for this Param or nil if there is none.
func (p Param) Default() Value {
	d, ok := p.Value.(*Disjunction)
	if !ok || d.NumDefaults != 1 {
		return nil
	}
	return d.Values[0]
}

func (x *Builtin) qualifiedName(c *OpContext) string {
	if x.Package != InvalidLabel {
		return x.Package.StringValue(c) + "." + x.Name
	}
	return x.Name
}

// Kind here represents the case where Builtin is used as a Validator.
func (x *Builtin) Kind() Kind {
	return FuncKind
}

func (x *Builtin) BareValidator() *BuiltinValidator {
	if len(x.Params) != 1 ||
		(x.Result != BoolKind && x.Result != BottomKind) {
		return nil
	}
	return &BuiltinValidator{Builtin: x}
}

// IsValidator reports whether b should be interpreted as a Validator for the
// given number of arguments.
func (b *Builtin) IsValidator(numArgs int) bool {
	return numArgs == len(b.Params)-1 &&
		b.Result&^BoolKind == 0 &&
		b.Params[numArgs].Default() == nil
}

func bottom(v Value) *Bottom {
	if x, ok := v.(*Vertex); ok {
		v = x.Value()
	}
	b, _ := v.(*Bottom)
	return b
}

func (x *Builtin) checkArgs(c *OpContext, p token.Pos, numArgs int) bool {
	if numArgs > len(x.Params) {
		c.addErrf(0, p,
			"too many arguments in call to %v (have %d, want %d)",
			x, numArgs, len(x.Params))
		return false
	}
	if numArgs < len(x.Params) {
		// Assume that all subsequent params have a default as well.
		v := x.Params[numArgs].Default()
		if v == nil {
			c.addErrf(0, p,
				"not enough arguments in call to %v (have %d, want %d)",
				x, numArgs, len(x.Params))
			return false
		}
	}
	return true
}

// completeArgs appends the arguments for the parameters a call omits. The
// CUE signatures of a builtin are the source of truth for its defaults: an
// omitted argument takes the default that a signature unified with the
// builtin declares for the parameter, or else the builtin's own default,
// which such a signature must agree with.
func (x *Builtin) completeArgs(c *OpContext, p token.Pos, args []Value, flags Flags) ([]Value, bool) {
	n := len(args)
	if n > len(x.Params) {
		c.addErrf(0, p,
			"too many arguments in call to %v (have %d, want %d)",
			x, n, len(x.Params))
		return nil, false
	}
	for i := n; i < len(x.Params); i++ {
		v, ok := x.signatureDefault(c, i, flags)
		if !ok {
			v = x.Params[i].Default()
		} else if v == nil || bottom(v) != nil {
			if b := bottom(v); b != nil {
				c.AddBottom(b)
			}
			return nil, false
		}
		if v == nil {
			c.addErrf(0, p,
				"not enough arguments in call to %v (have %d, want %d)",
				x, n, len(x.Params))
			return nil, false
		}
		args = append(args, v)
	}
	return args, true
}

// signatureDefault evaluates the default that a signature unified with the
// builtin declares for its parameter at position pos, and reports whether
// there is one.
func (x *Builtin) signatureDefault(c *OpContext, pos int, flags Flags) (Value, bool) {
	for _, t := range x.Types {
		for k, j := range matchBuiltinParams(t.Fn, x) {
			d := t.Fn.Params[k].Default
			if j != pos || d == nil {
				continue
			}
			s := c.PushState(t.Env, d.Source())
			v := c.value(d, flags)
			c.PopState(s)
			return v, true
		}
	}
	return nil, false
}

func (x *Builtin) call(call BuiltinCallContext) Expr {
	c := call.ctx
	p := call.Pos()

	fun := x // right now always x.
	if !x.checkArgs(c, p, len(call.args)) {
		return nil
	}
	for i := len(call.args); i < len(x.Params); i++ {
		call.args = append(call.args, x.Params[i].Default())
	}
	for i, a := range call.args {
		if b := bottom(a); b != nil {
			return b
		}
		if k := kind(a); x.Params[i].Kind()&k == BottomKind {
			code := EvalError
			b, _ := call.args[i].(*Bottom)
			if b != nil {
				code = b.Code
			}
			c.addErrf(code, Pos(a),
				"cannot use %s (type %s) as %s in argument %d to %v",
				a, k, x.Params[i].Kind(), i+1, fun)
			return nil
		}
		v := x.Params[i].Value
		if _, ok := v.(*BasicType); !ok {
			env := c.Env(0)
			x := &BinaryExpr{Op: AndOp, X: v, Y: a}
			n := c.newInlineVertex(nil, nil, Conjunct{env, x, c.ci})
			n.Finalize(c)
			if n.IsErr() {
				c.addErrf(0, Pos(a),
					"cannot use %s as %s in argument %d to %v",
					a, v, i+1, fun)
				return nil
			}
			call.args[i] = n
		}
	}

	// Arguments to functions are open. This mostly matters for NonConcrete
	// builtins.
	saved := c.isValidator
	c.isValidator = call.isValidator
	ci := c.ci
	c.ci.FromEmbed = false
	c.ci.FromDef = false
	defer func() {
		c.ci.FromDef = ci.FromDef
		c.ci.FromEmbed = ci.FromEmbed
		c.isValidator = saved
	}()

	if x.RawFunc != nil {
		return x.RawFunc(call)
	}
	return x.Func(call)
}

func (x *Builtin) Source() ast.Node { return nil }

// A BuiltinValidator is a Value that results from evaluation a partial call
// to a builtin (using CallExpr).
//
//	strings.MinRunes(4)
//
// ExternalFunc is a function supplied by the program embedding the evaluator,
// rather than one registered by a built-in package. Unlike [Builtin] it
// carries no package identity and supports no default arguments,
// non-concrete argument handling, or implicit validators: it is
// constructed at run time and called with concrete arguments.
type ExternalFunc struct {
	// Func is called with the evaluated concrete arguments.
	Func func(ctx *OpContext, args []Value) Expr

	Name string
}

func (f *ExternalFunc) Source() ast.Node { return nil }
func (f *ExternalFunc) Kind() Kind       { return FuncKind }

func (f *ExternalFunc) rawCall(c *OpContext, call *CallExpr, state Flags) Value {
	saved := c.ci
	c.ci.FromDef = false
	c.ci.FromEmbed = false
	defer func() {
		c.ci.FromDef = saved.FromDef
		c.ci.FromEmbed = saved.FromEmbed
	}()

	args := make([]Value, 0, len(call.Args))
	for i, a := range call.Args {
		savedErrs := c.errs
		c.errs = nil

		expr := c.value(a, Flags{
			status:    state.status,
			condition: state.condition | fieldSetKnown | concreteKnown | disjunctionTask,
			mode:      state.mode,
		})

		switch v := expr.(type) {
		case nil:
			if c.errs == nil {
				// There SHOULD be an error in the context. If not, we generate
				// one.
				c.Assertf(Pos(call.Fun), c.HasErr(),
					"argument %d to function %s is incomplete", i, call.Fun)
			}

		case *Bottom:
			c.errs = CombineErrors(a.Source(), c.errs, v)

		default:
			args = append(args, expr)
		}
		c.errs = CombineErrors(a.Source(), savedErrs, c.errs)
	}
	if c.HasErr() {
		return nil
	}

	result := f.Func(c, args)
	if result == nil {
		return nil
	}
	v, ci := c.evalStateCI(result, Flags{status: partial, condition: state.condition, mode: state.mode})
	c.ci = ci
	return v
}

// ExternalValidator is to [BuiltinValidator] what [ExternalFunc] is to
// [Builtin]: a
// validator supplied by the embedding program. When unified with a
// concrete value, Validate is called with it.
type ExternalValidator struct {
	Validate func(ctx *OpContext, v Value) *Bottom

	Name string
}

func (f *ExternalValidator) Source() ast.Node { return nil }
func (f *ExternalValidator) Kind() Kind       { return TopKind }

func (f *ExternalValidator) validate(c *OpContext, v Value) *Bottom {
	return f.Validate(c, v)
}

type BuiltinValidator struct {
	Src     *CallExpr
	Builtin *Builtin
	Args    []Value // any but the first value

	Env *Environment
}

func (x *BuiltinValidator) Source() ast.Node {
	if x.Src == nil {
		return x.Builtin.Source()
	}
	return x.Src.Source()
}

func (x *BuiltinValidator) Kind() Kind {
	return x.Builtin.Params[0].Kind()
}

func (x *BuiltinValidator) validate(c *OpContext, v Value) *Bottom {
	args := make([]Value, len(x.Args)+1)
	args[0] = v
	copy(args[1:], x.Args)

	return validateWithBuiltin(BuiltinCallContext{
		ctx:         c,
		call:        x.Src,
		builtin:     x.Builtin,
		args:        args,
		isValidator: true,
	})
}

func validateWithBuiltin(call BuiltinCallContext) *Bottom {
	var severeness ErrorCode
	var err errors.Error

	c := call.ctx
	b := call.builtin
	src := call.Pos()
	arg0 := call.Value(0)

	res := call.builtin.call(call)
	switch v := res.(type) {
	case nil:
		return nil

	case *Bottom:
		if v == nil {
			return nil // caught elsewhere, but be defensive.
		}
		severeness = v.Code
		err = v.Err

	case *Bool:
		if v.B {
			return nil
		}

	default:
		return c.NewErrf("invalid validator %s", b.qualifiedName(c))
	}

	// If the validator returns an error and we already had an error, just
	// return the original error.
	if b, ok := Unwrap(call.Value(0)).(*Bottom); ok {
		return b
	}
	// failed:
	// TODO(mvdan): building this buffer should be part of the error format and arguments,
	// e.g. any logic needed here can be wrapped in an [fmt.Stringer].
	var buf bytes.Buffer
	buf.WriteString(b.qualifiedName(c))

	// Note: when the builtin accepts non-concrete arguments, omit them because
	// they can easily be very large.
	if !b.NonConcrete && call.NumParams() > 1 { // use NumArgs instead
		buf.WriteString("(")
		// TODO: use accessor instead of call.arg
		for i, a := range call.args[1:] {
			if i > 0 {
				_, _ = buf.WriteString(", ")
			}
			buf.WriteString(c.String(a))
		}
		buf.WriteString(")")
	}

	vErr := c.NewPosf(src, "invalid value %s (does not satisfy %s)", arg0, buf.String())

	call.AddPositions(vErr)

	return &Bottom{
		Code: severeness,
		Err:  errors.Wrap(vErr, err),
		Node: c.vertex,
	}
}

// A DisjunctionExpr represents a disjunction, where each disjunct may or may not
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

func (x *DisjunctionExpr) evaluate(c *OpContext, state Flags) Value {
	e := c.Env(0)
	v := c.newInlineVertex(nil, nil, Conjunct{e, x, c.ci})
	v.Finalize(c) // TODO: also partial okay?
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

// A Disjunction is a disjunction of values. It is the result of expanding
// a [DisjunctionExpr] if the expression cannot be represented as a single value.
type Disjunction struct {
	Src ast.Expr

	// Values are the non-error disjuncts of this expression. The first
	// NumDefaults values are default values.
	Values []Value

	Errors *Bottom // []bottom

	// NumDefaults indicates the number of default values.
	NumDefaults int
	HasDefaults bool

	// owner, if set, is the Vertex whose disjuncts Values holds. Cloned
	// vertices share BaseValue with the vertex they were cloned from; only
	// the owner may reclaim the Values. See
	// [reclaimer.reclaimBaseValueBuffers].
	//
	// TODO(mem): consider deriving ownership on the reclaim side instead of
	// storing it here.
	owner *Vertex
}

func (x *Disjunction) Source() ast.Node { return x.Src }
func (x *Disjunction) Kind() Kind {
	k := BottomKind
	for _, v := range x.Values {
		k |= v.Kind()
	}
	return k
}

type Comprehension struct {
	Syntax ast.Node

	// Clauses is the list of for, if, and other clauses of a comprehension,
	// not including the yielded value (in curly braces).
	Clauses []Yielder

	// Value is the body struct yielded once per evaluation of the
	// comprehension's clauses. With dependency-tracking pushdown, the
	// compiler always lowers the body to a *StructLit (Fields and other
	// declarations live inside it as decls), so this is no longer the
	// polymorphic Node it once was.
	Value *StructLit

	// Fallback is the optional else clause that is yielded when the comprehension
	// produces zero values.
	Fallback *StructLit
}

func (x *Comprehension) Source() ast.Node {
	if x.Syntax == nil {
		return nil
	}
	return x.Syntax
}

// A ForClause represents a for clause of a comprehension. It can be used
// as a struct or list element.
//
//	for k, v in src {}
type ForClause struct {
	Syntax *ast.ForClause
	Key    Feature
	Value  Feature
	Src    Expr
}

func (x *ForClause) Source() ast.Node {
	if x.Syntax == nil {
		return nil
	}
	return x.Syntax
}

func (c *OpContext) forSource(x Expr) *Vertex {
	state := Flags{
		status:    conjuncts,
		condition: needFieldSetKnown,
		mode:      attemptOnly,
	}

	// TODO: always get the vertex. This allows a whole bunch of trickery
	// down the line.
	c.inDetached++
	v := c.unifyNode(x, state)
	c.inDetached--

	node, ok := v.(*Vertex)
	if ok {
		// By default we rely on the call-by-need behavior in combination
		// with the freezing mechanism as the cycle-breaker, rather than
		// yielding. The exception is a source that a sibling parent task
		// (e.g. an if-comprehension whose guard is still being evaluated)
		// may yet add fields to: finalizing now would freeze an incomplete
		// field set, so yield and retry once the sibling completes.
		// TODO: this seems a bit fragile. At some point we need to make this
		// more robust by moving to a pure call-by-need mechanism, for instance.
		// TODO: using attemptOnly here will remove the cyclic reference error
		// of comprehension.t1.ok (which also errors in V2),
		mode := finalize
		if s := node.state; s != nil && s.frozen&fieldSetKnown == 0 &&
			s.hasRunningSiblingParentTask() {
			mode = yield
		}
		node.unify(c, Flags{condition: state.condition, mode: mode, checkTypos: true})
	}

	v, ok = c.getDefault(v)

	if !ok {
		// Error already generated by getDefault.
		return emptyNode
	}

	// TODO: skip in new evaluator? Check once we introduce disjunctions.
	if w := Unwrap(v); !isCyclePlaceholder(w) {
		v = w
	}
	node, ok = v.(*Vertex)
	if ok && !isCyclePlaceholder(node.BaseValue) {
		v = node.Value()
	}

	switch nv := v.(type) {
	case nil:
		c.addErrf(IncompleteError, Pos(x),
			"cannot range over %s (incomplete)", x)
		return emptyNode

	case *Bottom:
		// TODO: this is a bit messy. In some cases errors are already added
		// and in some cases not. Not a huge deal, as errors will be uniqued
		// down the line, but could be better.
		c.AddBottom(nv)
		return emptyNode

	case *Vertex:
		if node == nil {
			panic("unexpected markers with nil node")
		}

	default:
		if kind := v.Kind(); kind&(StructKind|ListKind) != 0 {
			c.addErrf(IncompleteError, Pos(x),
				"cannot range over %s (incomplete type %s)", x, kind)
			return emptyNode

		} else if !ok {
			c.addErrf(0, Pos(x), // TODO(error): better message.
				"cannot range over %s (found %s, want list or struct)",
				x.Source(), v.Kind())
			return emptyNode
		}
	}
	kind := v.Kind()
	// At this point it is possible that the Vertex represents an incomplete
	// struct or list, which is the case if it may be struct or list, but
	// is also at least some other type, such as is the case with top.
	if kind&(StructKind|ListKind) != 0 && kind != StructKind && kind != ListKind {
		c.addErrf(IncompleteError, Pos(x),
			"cannot range over %s (incomplete type %s)", x, kind)
		return emptyNode
	}

	return node
}

func (x *ForClause) yield(s *compState) {
	c := s.ctx
	env := c.Env(0)
	n := c.forSource(x.Src)

	if s := n.getState(c); s != nil {
		s.freeze(fieldSetKnown, x)
	}

	for _, a := range n.Arcs {
		if !a.Label.IsRegular() {
			continue
		}

		// See comment in StructLit.evaluate.
		if state := a.getState(c); state != nil {
			state.process(arcTypeKnown, attemptOnly)
		}

		switch a.ArcType {
		case ArcMember:
		case ArcRequired:
			c.AddBottom(newRequiredFieldInComprehensionError(c, x, a))
			continue
		default:
			continue
		}

		// "for" clauses tend to yield many values;
		// group allocations with the same lifetime here
		// for the sake of reducing the runtime overhead.
		alloc := struct {
			v0, v1, v2 Vertex
			arcs       [2]*Vertex
			env        Environment
		}{}

		n := &alloc.v0
		*n = Vertex{
			Parent: env.DerefVertex(c),

			// Using Finalized here ensures that no nodeContext is allocated,
			// preventing a leak, as this "helper" struct bypasses normal
			// processing, eluding the deallocation step.
			status:    finalized,
			IsDynamic: true,
			anonymous: true,
			ArcType:   ArcMember,

			Arcs: alloc.arcs[:0],
		}

		if x.Value != InvalidLabel {
			b := &alloc.v1
			*b = Vertex{
				Label:     x.Value,
				BaseValue: a,
				IsDynamic: true,
				anonymous: true,
				ArcType:   ArcPending,
			}
			n.Arcs = append(n.Arcs, b)
		}

		if x.Key != InvalidLabel {
			v := &alloc.v2
			*v = Vertex{
				Label:     x.Key,
				IsDynamic: true,
				anonymous: true,
			}
			var key Value
			if a.Label.IsString() {
				key = &String{Src: c.src, Str: c.IndexToString(a.Label.safeIndex())}
			} else {
				num := &Num{Src: c.src, K: IntKind}
				num.X.SetInt64(int64(a.Label.Index()))
				key = num
			}
			v.AddConjunct(MakeRootConjunct(env, key))
			v.SetValue(c, key)
			n.Arcs = append(n.Arcs, v)
		}

		sub := &alloc.env
		*sub = Environment{
			Up:     env,
			Vertex: n,
			CompID: s.compID,
		}
		if !s.yield(sub) {
			break
		}
	}
}

// An IfClause represents an if clause of a comprehension. It can be used
// as a struct or list element.
//
//	if cond {}
type IfClause struct {
	Src       *ast.IfClause
	Condition Expr
}

func (x *IfClause) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *IfClause) yield(s *compState) {
	ctx := s.ctx
	if ctx.BoolValue(ctx.value(x.Condition, Flags{
		status:    s.state,
		condition: scalarKnown,
		mode:      yield,
	})) {
		s.yield(ctx.e)
	}
}

// A LetClause represents a let clause in a comprehension.
//
//	let x = y
type LetClause struct {
	Src   *ast.LetClause
	Label Feature
	Expr  Expr
}

func (x *LetClause) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *LetClause) yield(s *compState) {
	c := s.ctx
	n := &Vertex{Arcs: []*Vertex{
		{
			Label:     x.Label,
			IsDynamic: true,
			anonymous: true,
			Conjuncts: []Conjunct{{c.Env(0), x.Expr, c.ci}},
		},
	}}

	s.yield(s.spawn(n))
}

// A TryClause represents a try clause in a comprehension.
// It evaluates its body and yields if successful. If a ?-marked reference
// fails due to an undefined optional field, the try clause discards silently.
// Other errors propagate normally.
//
//	try { ... }
//
// The body is pre-evaluated in isolation in an inline vertex, which only
// approximates its real evaluation. Several mechanisms compensate for the
// differences: a failed reference is attributed to its body by ancestry
// ([OpContext.markSkipTry]), tasks blocked on rooted vertices are left
// alone ([scheduler.inTryBody]), and a lookup through a field the body
// declares falls back to the struct it is inserted into
// ([OpContext.lookupInTryTarget]).
//
// TryClause represents a try clause in a comprehension.
// It can have two forms:
//   - try { struct } - Value is set, Label/Expr are zero/nil
//   - try x = expr   - Label/Expr are set, Value is nil
type TryClause struct {
	Src   *ast.TryClause
	Label Feature // identifier for assignment form (InvalidLabel for struct form)
	Expr  Expr    // expression for assignment form (nil for struct form)
	// Struct form: body is in Comprehension.Value
}

// tryBody is the state of the pre-evaluation of a try clause body, held by the
// [nodeContext] of the inline vertex evaluating it.
type tryBody struct {
	// skip records that a ?-marked reference in the body failed to resolve.
	// See [OpContext.markSkipTry].
	skip bool

	// target is the vertex a struct-form body is inserted into when it
	// yields, and nil for the binding form, whose expression is not inserted
	// anywhere. See [OpContext.lookupInTryTarget].
	target *Vertex
}

func (x *TryClause) Source() ast.Node {
	if x.Src == nil {
		return nil
	}
	return x.Src
}

func (x *TryClause) yield(s *compState) {
	c := s.ctx
	env := c.e

	// Pre-evaluate the try body to detect OptionalUndefined errors from
	// ?-marked references. If any ?-marked reference fails, the try block
	// is discarded and the else clause (if present) runs.
	//
	// Final (non-incomplete) errors are reported immediately as an optimization,
	// since they would be encountered during re-evaluation anyway.

	// TODO(perf): we could capture "final" errors and bail out processing of
	// the try expression early.

	var expr Expr
	tb := &tryBody{}
	if x.Expr != nil {
		expr = x.Expr
	} else {
		// Struct form: the body is inserted into the node the comprehension
		// runs on, which [OpContext.yield] set as the current vertex.
		expr = s.comp.Value
		tb.target = c.vertex
	}

	v := c.newInlineVertex(env.DerefVertex(c), nil, Conjunct{env, expr, c.ci})

	// Mark this body so a failed ?-marked reference is attributed to this try
	// rather than an enclosing or interleaving one (see markSkipTry), and so
	// that finalizing it leaves tasks blocked on rooted vertices alone (see
	// [scheduler.inTryBody]).
	v.getState(c).tryBody = tb
	v.Finalize(c)

	// If any ?-marked reference belonging to this body failed, don't yield -
	// the else clause (if present) runs instead.
	if tb.skip {
		return
	}

	// Success - yield with fresh conjuncts (will be re-evaluated).
	if x.Expr != nil {
		n := &Vertex{Arcs: []*Vertex{{
			Label:     x.Label,
			IsDynamic: true,
			anonymous: true,
			Conjuncts: []Conjunct{{c.Env(0), x.Expr, c.ci}},
		}}}
		s.yield(s.spawn(n))
	} else {
		s.yield(env)
	}
}
