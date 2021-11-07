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
	"testing"
)

func TestNilSource(t *testing.T) {
	testCases := []Node{
		&BasicType{},
		&BinaryExpr{},
		&Bool{},
		&Bottom{},
		&BoundExpr{},
		&BoundValue{},
		&Builtin{},
		&BuiltinValidator{},
		&BulkOptionalField{},
		&Bytes{},
		&CallExpr{},
		&Comprehension{},
		&Conjunction{},
		&Disjunction{},
		&DisjunctionExpr{},
		&DynamicField{},
		&DynamicReference{},
		&Ellipsis{},
		&Field{},
		&FieldReference{},
		&ForClause{},
		&IfClause{},
		&ImportReference{},
		&IndexExpr{},
		&Interpolation{},
		&LabelReference{},
		&LetClause{},
		&LetReference{},
		&ListLit{},
		&ListMarker{},
		&NodeLink{},
		&Null{},
		&Num{},
		&OptionalField{},
		&SelectorExpr{},
		&SliceExpr{},
		&String{},
		&StructLit{},
		&StructMarker{},
		&Top{},
		&UnaryExpr{},
		&ValueClause{},
		&Vertex{},
	}
	for _, x := range testCases {
		t.Run(fmt.Sprintf("%T", x), func(t *testing.T) {
			if x.Source() != nil {
				t.Error("nil source did not return nil")
			}
		})
	}
}
