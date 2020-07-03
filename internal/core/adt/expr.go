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

func (x *Field) Source() ast.Node { return x.Src }

// An OptionalField represents an optional regular field.
//
//   foo?: expr
//
type OptionalField struct {
	Src   *ast.Field
	Label Feature
	Value Expr
}

func (x *OptionalField) Source() ast.Node { return x.Src }

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

func (x *BulkOptionalField) Source() ast.Node { return x.Src }

// A Ellipsis represents a set of optional fields of a given type.
//
//   ...T
//
type Ellipsis struct {
	Src   *ast.Ellipsis
	Value Expr
}

func (x *Ellipsis) Source() ast.Node { return x.Src }

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

func (x *DynamicField) Source() ast.Node { return x.Src }

// Expressions

// A ListLit represents an unevaluated list literal.
//
//    [a, for x in src { ... }, b, ...T]
//
type ListLit struct {
	Src *ast.ListLit

	// scalars, comprehensions, ...T
	Elems []Elem
}

func (x *ListLit) Source() ast.Node { return x.Src }

// -- Literals

// Bottom represents an error or bottom symbol.
type Bottom struct {
	Src ast.Node
	Err errors.Error
}

func (x *Bottom) Source() ast.Node { return x.Src }
func (x *Bottom) Kind() Kind       { return BottomKind }

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

func (x *Num) Source() ast.Node { return x.Src }
func (x *Num) Kind() Kind       { return x.K }

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

// -- composites: the evaluated fields of a composite are recorded in the arc
// vertices.

type ListMarker struct {
	Src ast.Node
}

func (x *ListMarker) Source() ast.Node { return x.Src }
func (x *ListMarker) Kind() Kind       { return ListKind }
func (x *ListMarker) node()            {}

type StructMarker struct {
	Closed bool
}

func (x *StructMarker) Source() ast.Node { return nil }
func (x *StructMarker) Kind() Kind       { return StructKind }
func (x *StructMarker) node()            {}

// -- top types

// Top represents all possible values. It can be used as a Value and Expr.
type Top struct{ Src *ast.Ident }

func (x *Top) Source() ast.Node { return x.Src }
func (x *Top) Kind() Kind       { return TopKind }

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

func (x *BasicType) Source() ast.Node { return x.Src }
func (x *BasicType) Kind() Kind       { return x.K }

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

func (x *BoundExpr) Source() ast.Node { return x.Src }

// A BoundValue is a fully evaluated unary comparator that can be used to
// validate other values.
//
//    <5
//    =~"Name$"
//
type BoundValue struct {
	Src   ast.Node
	Op    Op
	Value Value
	K     Kind
}

func (x *BoundValue) Source() ast.Node { return x.Src }
func (x *BoundValue) Kind() Kind       { return x.K }

// -- References

// A FieldReference represents a lexical reference to a field.
//
//    a
//
type FieldReference struct {
	Src     *ast.Ident
	UpCount int32
	Label   Feature
}

func (x *FieldReference) Source() ast.Node { return x.Src }

// A LabelReference refers to the string or integer value of a label.
//
//    [X=Pattern]: b: X
//
type LabelReference struct {
	Src     *ast.Ident
	UpCount int32
}

// TODO: should this implement resolver at all?

func (x *LabelReference) Source() ast.Node { return x.Src }

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

func (x *DynamicReference) Source() ast.Node { return x.Src }

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

func (x *ImportReference) Source() ast.Node { return x.Src }

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

func (x *LetReference) Source() ast.Node { return x.Src }

// A SelectorExpr looks up a fixed field in an expression.
//
//     X.Sel
//
type SelectorExpr struct {
	Src *ast.SelectorExpr
	X   Expr
	Sel Feature
}

func (x *SelectorExpr) Source() ast.Node { return x.Src }

// IndexExpr is like a selector, but selects an index.
//
//    X[Index]
//
type IndexExpr struct {
	Src   *ast.IndexExpr
	X     Expr
	Index Expr
}

func (x *IndexExpr) Source() ast.Node { return x.Src }

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

func (x *SliceExpr) Source() ast.Node { return x.Src }

// An Interpolation is a string interpolation.
//
//    "a \(b) c"
//
type Interpolation struct {
	Src   *ast.Interpolation
	K     Kind   // string or bytes
	Parts []Expr // odd: strings, even sources
}

func (x *Interpolation) Source() ast.Node { return x.Src }

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

func (x *UnaryExpr) Source() ast.Node { return x.Src }

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

func (x *BinaryExpr) Source() ast.Node { return x.Src }

// -- builtins

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

func (x *CallExpr) Source() ast.Node { return x.Src }

// A BuiltinValidator is a Value that results from evaluation a partial call
// to a builtin (using CallExpr).
//
//    strings.MinRunes(4)
//
type BuiltinValidator struct {
	Src  *ast.CallExpr
	Fun  Expr    // TODO: should probably be builtin.
	Args []Value // any but the first value
	// call *builtin // function must return a bool
}

func (x *BuiltinValidator) Source() ast.Node { return x.Src }
func (x *BuiltinValidator) Kind() Kind       { return TopKind }

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

// A Conjunction is a conjunction of values that cannot be represented as a
// single value. It is the result of unification.
type Conjunction struct {
	Values []Value
}

func (x *Conjunction) Source() ast.Node { return nil }
func (x *Conjunction) Kind() Kind       { return TopKind }

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

func (x *ForClause) Source() ast.Node { return x.Syntax }

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

func (x *IfClause) Source() ast.Node { return x.Src }

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

func (x *LetClause) Source() ast.Node { return x.Src }

// A ValueClause represents the value part of a comprehension.
type ValueClause struct {
	*StructLit
}

func (x *ValueClause) Source() ast.Node { return x.Src }
