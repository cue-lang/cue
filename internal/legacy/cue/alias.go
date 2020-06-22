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

package cue

import "cuelang.org/go/internal/core/adt"

type (
	bottom    = adt.Bottom
	source    = adt.Node
	kind      = adt.Kind
	nullLit   = adt.Null
	boolLit   = adt.Bool
	numLit    = adt.Num
	stringLit = adt.String
	bytesLit  = adt.Bytes
	structLit = adt.Vertex

	arc       = *adt.Vertex
	value     = adt.Expr
	evaluated = adt.Value
	label     = adt.Feature

	listLit         = adt.ListLit
	top             = adt.Top
	basicType       = adt.BasicType
	boundExpr       = adt.BoundExpr
	boundValue      = adt.BoundValue
	selectorExpr    = adt.SelectorExpr
	indexExpr       = adt.IndexExpr
	sliceExpr       = adt.SliceExpr
	interpolation   = adt.Interpolation
	unaryExpr       = adt.UnaryExpr
	binaryExpr      = adt.BinaryExpr
	callExpr        = adt.CallExpr
	disjunction     = adt.DisjunctionExpr
	dValue          = adt.Disjunct
	customValidator = adt.BuiltinValidator
)

const (
	topKind    = adt.TopKind
	nullKind   = adt.NullKind
	boolKind   = adt.BoolKind
	numKind    = adt.NumKind
	intKind    = adt.IntKind
	floatKind  = adt.FloatKind
	stringKind = adt.StringKind
	bytesKind  = adt.BytesKind
	listKind   = adt.ListKind
	structKind = adt.StructKind
	bottomKind = adt.BottomKind
)
