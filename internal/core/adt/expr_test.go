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

package adt_test

import (
	"fmt"
	"testing"

	"cuelang.org/go/internal/core/adt"
)

func TestNilSource(t *testing.T) {
	testCases := []adt.Node{
		&adt.BasicType{},
		&adt.BinaryExpr{},
		&adt.Bool{},
		&adt.Bottom{},
		&adt.BoundExpr{},
		&adt.BoundValue{},
		&adt.Builtin{},
		&adt.BuiltinValidator{},
		&adt.BulkOptionalField{},
		&adt.Bytes{},
		&adt.CallExpr{},
		&adt.Comprehension{},
		&adt.Conjunction{},
		&adt.Disjunction{},
		&adt.DisjunctionExpr{},
		&adt.DynamicField{},
		&adt.DynamicReference{},
		&adt.Ellipsis{},
		&adt.Field{},
		&adt.FieldReference{},
		&adt.ForClause{},
		&adt.IfClause{},
		&adt.ImportReference{},
		&adt.IndexExpr{},
		&adt.Interpolation{},
		&adt.LabelReference{},
		&adt.LetClause{},
		&adt.LetField{},
		&adt.LetReference{},
		&adt.ListLit{},
		&adt.ListMarker{},
		&adt.NodeLink{},
		&adt.Null{},
		&adt.Num{},
		&adt.SelectorExpr{},
		&adt.SliceExpr{},
		&adt.String{},
		&adt.StructLit{},
		&adt.StructMarker{},
		&adt.Top{},
		&adt.UnaryExpr{},
		&adt.Vertex{},
	}
	for _, x := range testCases {
		t.Run(fmt.Sprintf("%T", x), func(t *testing.T) {
			if x.Source() != nil {
				t.Error("nil source did not return nil")
			}
		})
	}
}

func TestNumBigInt(t *testing.T) {
	// The evaluator keeps integers with a zero exponent, but an integer
	// decimal need not have one, so BigInt must scale by it. See
	// https://cuelang.org/issue/2649 and https://cuelang.org/issue/3787 for
	// the two ways such values used to arise.
	testCases := []struct {
		decimal string
		want    string
	}{
		{"8", "8"},
		{"-8", "-8"},
		{"8.0", "8"},
		{"-8.0", "-8"},
		{"80E-1", "8"},
		{"1.0E+2", "100"},
		{"1E+35", "100000000000000000000000000000000000"},
		{"-1E+35", "-100000000000000000000000000000000000"},
		{"0.0", "0"},
	}
	for _, tc := range testCases {
		t.Run(tc.decimal, func(t *testing.T) {
			n := &adt.Num{K: adt.IntKind}
			if _, _, err := n.X.SetString(tc.decimal); err != nil {
				t.Fatal(err)
			}
			if got := n.BigInt(nil).String(); got != tc.want {
				t.Errorf("BigInt() = %v; want %v", got, tc.want)
			}
		})
	}
}
