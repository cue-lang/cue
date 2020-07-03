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

import "cuelang.org/go/cue/ast"

// A Node is any abstract data type representing an value or expression.
type Node interface {
	Source() ast.Node
	node() // enforce internal.
}

// A Decl represents all valid StructLit elements.
type Decl interface {
	Node
	declNode()
}

// An Elem represents all value ListLit elements.
//
// All Elem values can be used as a Decl.
type Elem interface {
	Decl
	elemNode()
}

// An Expr corresponds to an ast.Expr.
//
// All Expr values can be used as an Elem or Decl.
type Expr interface {
	Elem
	expr()
}

// A Value represents a node in the evaluated data graph.
//
// All Values values can also be used as a Expr.
type Value interface {
	Expr
	Concreteness() Concreteness
	Kind() Kind
}

// An Evaluator provides a method to convert to a value.
type Evaluator interface {
	Node
	// TODO: Eval(c Context, env *Environment) Value
}

// A Resolver represents a reference somewhere else within a tree that resolves
// a value.
type Resolver interface {
	Node
	// TODO: Resolve(c Context, env *Environment) Arc
}

// A Yielder represents 0 or more labeled values of structs or lists.
type Yielder interface {
	Node
	yielderNode()
	// TODO: Yield()
}

// A Validator validates a Value. All Validators are Values.
type Validator interface {
	// TODO: Validate(c Context, v Value) *Bottom
	Value
}

// Value

func (x *Vertex) Concreteness() Concreteness {
	// Depends on concreteness of value.
	if x.Value == nil {
		return Concrete // Should be indetermined.
	}
	return x.Value.Concreteness()
}

func (x *ListMarker) Concreteness() Concreteness   { return Concrete }
func (x *StructMarker) Concreteness() Concreteness { return Concrete }

func (*Conjunction) Concreteness() Concreteness      { return Constraint }
func (*Disjunction) Concreteness() Concreteness      { return Constraint }
func (*BoundValue) Concreteness() Concreteness       { return Constraint }
func (*BuiltinValidator) Concreteness() Concreteness { return Constraint }

// Value and Expr

func (*Bottom) Concreteness() Concreteness    { return BottomLevel }
func (*Null) Concreteness() Concreteness      { return Concrete }
func (*Bool) Concreteness() Concreteness      { return Concrete }
func (*Num) Concreteness() Concreteness       { return Concrete }
func (*String) Concreteness() Concreteness    { return Concrete }
func (*Bytes) Concreteness() Concreteness     { return Concrete }
func (*Top) Concreteness() Concreteness       { return Any }
func (*BasicType) Concreteness() Concreteness { return Type }

// Expr

func (*StructLit) expr()       {}
func (*ListLit) expr()         {}
func (*DisjunctionExpr) expr() {}

// Expr and Value

func (*Bottom) expr()           {}
func (*Null) expr()             {}
func (*Bool) expr()             {}
func (*Num) expr()              {}
func (*String) expr()           {}
func (*Bytes) expr()            {}
func (*Top) expr()              {}
func (*BasicType) expr()        {}
func (*Vertex) expr()           {}
func (*ListMarker) expr()       {}
func (*StructMarker) expr()     {}
func (*Conjunction) expr()      {}
func (*Disjunction) expr()      {}
func (*BoundValue) expr()       {}
func (*BuiltinValidator) expr() {}

// Expr and Resolver

func (*FieldReference) expr()   {}
func (*LabelReference) expr()   {}
func (*DynamicReference) expr() {}
func (*ImportReference) expr()  {}
func (*LetReference) expr()     {}

// Expr and Evaluator

func (*BoundExpr) expr()     {}
func (*SelectorExpr) expr()  {}
func (*IndexExpr) expr()     {}
func (*SliceExpr) expr()     {}
func (*Interpolation) expr() {}
func (*UnaryExpr) expr()     {}
func (*BinaryExpr) expr()    {}
func (*CallExpr) expr()      {}

// Decl and Expr (so allow attaching original source in Conjunct)

func (*Field) declNode()                {}
func (x *Field) expr() Expr             { return x.Value }
func (*OptionalField) declNode()        {}
func (x *OptionalField) expr() Expr     { return x.Value }
func (*BulkOptionalField) declNode()    {}
func (x *BulkOptionalField) expr() Expr { return x.Value }
func (*DynamicField) declNode()         {}
func (x *DynamicField) expr() Expr      { return x.Value }

// Decl and Yielder

func (*LetClause) declNode()    {}
func (*LetClause) yielderNode() {}

// Decl and Elem

func (*StructLit) declNode()        {}
func (*StructLit) elemNode()        {}
func (*ListLit) declNode()          {}
func (*ListLit) elemNode()          {}
func (*Ellipsis) elemNode()         {}
func (*Ellipsis) declNode()         {}
func (*Bottom) declNode()           {}
func (*Bottom) elemNode()           {}
func (*Null) declNode()             {}
func (*Null) elemNode()             {}
func (*Bool) declNode()             {}
func (*Bool) elemNode()             {}
func (*Num) declNode()              {}
func (*Num) elemNode()              {}
func (*String) declNode()           {}
func (*String) elemNode()           {}
func (*Bytes) declNode()            {}
func (*Bytes) elemNode()            {}
func (*Top) declNode()              {}
func (*Top) elemNode()              {}
func (*BasicType) declNode()        {}
func (*BasicType) elemNode()        {}
func (*BoundExpr) declNode()        {}
func (*BoundExpr) elemNode()        {}
func (*Vertex) declNode()           {}
func (*Vertex) elemNode()           {}
func (*ListMarker) declNode()       {}
func (*ListMarker) elemNode()       {}
func (*StructMarker) declNode()     {}
func (*StructMarker) elemNode()     {}
func (*Conjunction) declNode()      {}
func (*Conjunction) elemNode()      {}
func (*Disjunction) declNode()      {}
func (*Disjunction) elemNode()      {}
func (*BoundValue) declNode()       {}
func (*BoundValue) elemNode()       {}
func (*BuiltinValidator) declNode() {}
func (*BuiltinValidator) elemNode() {}
func (*FieldReference) declNode()   {}
func (*FieldReference) elemNode()   {}
func (*LabelReference) declNode()   {}
func (*LabelReference) elemNode()   {}
func (*DynamicReference) declNode() {}
func (*DynamicReference) elemNode() {}
func (*ImportReference) declNode()  {}
func (*ImportReference) elemNode()  {}
func (*LetReference) declNode()     {}
func (*LetReference) elemNode()     {}
func (*SelectorExpr) declNode()     {}
func (*SelectorExpr) elemNode()     {}
func (*IndexExpr) declNode()        {}
func (*IndexExpr) elemNode()        {}
func (*SliceExpr) declNode()        {}
func (*SliceExpr) elemNode()        {}
func (*Interpolation) declNode()    {}
func (*Interpolation) elemNode()    {}
func (*UnaryExpr) declNode()        {}
func (*UnaryExpr) elemNode()        {}
func (*BinaryExpr) declNode()       {}
func (*BinaryExpr) elemNode()       {}
func (*CallExpr) declNode()         {}
func (*CallExpr) elemNode()         {}
func (*DisjunctionExpr) declNode()  {}
func (*DisjunctionExpr) elemNode()  {}

// Decl, Elem, and Yielder

func (*ForClause) declNode()    {}
func (*ForClause) elemNode()    {}
func (*ForClause) yielderNode() {}
func (*IfClause) declNode()     {}
func (*IfClause) elemNode()     {}
func (*IfClause) yielderNode()  {}

// Yielder

func (*ValueClause) yielderNode() {}

// Node

func (*Vertex) node()            {}
func (*Conjunction) node()       {}
func (*Disjunction) node()       {}
func (*BoundValue) node()        {}
func (*BuiltinValidator) node()  {}
func (*Bottom) node()            {}
func (*Null) node()              {}
func (*Bool) node()              {}
func (*Num) node()               {}
func (*String) node()            {}
func (*Bytes) node()             {}
func (*Top) node()               {}
func (*BasicType) node()         {}
func (*StructLit) node()         {}
func (*ListLit) node()           {}
func (*BoundExpr) node()         {}
func (*FieldReference) node()    {}
func (*LabelReference) node()    {}
func (*DynamicReference) node()  {}
func (*ImportReference) node()   {}
func (*LetReference) node()      {}
func (*SelectorExpr) node()      {}
func (*IndexExpr) node()         {}
func (*SliceExpr) node()         {}
func (*Interpolation) node()     {}
func (*UnaryExpr) node()         {}
func (*BinaryExpr) node()        {}
func (*CallExpr) node()          {}
func (*DisjunctionExpr) node()   {}
func (*Field) node()             {}
func (*OptionalField) node()     {}
func (*BulkOptionalField) node() {}
func (*DynamicField) node()      {}
func (*Ellipsis) node()          {}
func (*ForClause) node()         {}
func (*IfClause) node()          {}
func (*LetClause) node()         {}
func (*ValueClause) node()       {}
