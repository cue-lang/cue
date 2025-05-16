// Copyright 2025 CUE Authors
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

package cuedata

import (
	"encoding/json"
	"math/big"
	"testing"

	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/format"
	"github.com/go-quicktest/qt"
)

func TestEncode(t *testing.T) {
	type sample struct {
		A int    `json:"a"`
		B string `json:"b,omitempty"`
	}

	p := &sample{A: 2, B: ""}

	tests := []struct {
		testName string
		in       any
		wantErr  string
	}{
		{testName: "BoolTrue", in: true},
		{testName: "Int", in: int(42)},
		{testName: "Int8", in: int8(-3)},
		{testName: "Uint", in: uint(7)},
		{testName: "BigInt", in: big.NewInt(1234567890123456789)},
		{testName: "BigFloat", in: big.NewFloat(3.14159)},
		{testName: "JSONMarshaler", in: jsonInt(4)},
		{testName: "TextMarshaler", in: textVal("hello")},
		{testName: "SliceInt", in: []int{1, 2, 3}},
		{testName: "NilSlice", in: ([]int)(nil)},
		{testName: "Map", in: map[string]int{"x": 1, "y": 2}},
		{testName: "Struct", in: sample{A: 1, B: ""}},
		{testName: "Pointer", in: p},
		{testName: "NilPointer", in: (*sample)(nil)},
		{testName: "ByteSlice", in: []byte{0x01, 0x02, 0xff}},
		{testName: "NilInterface", in: (interface{})(nil)},
		{testName: "UnsupportedChannel", in: make(chan int), wantErr: "cuedata: unsupported Go kind chan"},
		{testName: "EmbeddedStruct", in: S2{
			S1: S1{A: 1234},
			B:  true,
		}},
	}

	ctx := cuecontext.New()
	for _, tt := range tests {
		t.Run(tt.testName, func(t *testing.T) {
			expr, err := Encode(tt.in)
			if tt.wantErr != "" {
				qt.Assert(t, qt.ErrorMatches(err, tt.wantErr))
				return
			}
			qt.Assert(t, qt.IsNil(err))

			gotSrc, err := format.Node(expr)
			qt.Assert(t, qt.IsNil(err))

			exprv := ctx.Encode(tt.in)
			qt.Assert(t, qt.IsNil(exprv.Err()))
			wantSrc, err := format.Node(exprv.Syntax())
			qt.Assert(t, qt.IsNil(err))

			qt.Assert(t, qt.DeepEquals(gotSrc, wantSrc))
		})
	}
}

type S1 struct {
	A int `json:"a"`
}

type S2 struct {
	S1
	B bool `json:"b"`
}

type jsonInt int

func (j jsonInt) MarshalJSON() ([]byte, error) {
	// Marshal as the underlying int + 1 so we can see the transformation.
	return json.Marshal(int(j) + 1)
}

type textVal string

func (t textVal) MarshalText() ([]byte, error) {
	return []byte(string(t) + "_txt"), nil
}
