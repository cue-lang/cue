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
	"fmt"
	"math/big"
	"testing"

	"github.com/cockroachdb/apd/v2"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
)

var testBase = newExpr(&ast.BasicLit{})

func mkInt(a int64) *numLit {
	return newInt(testBase, base10).setInt64(a)
}
func mkIntString(a string) *numLit {
	return newInt(testBase, base10).setString(a)
}
func mkFloat(a string) *numLit {
	return newFloat(testBase, base10).setString(a)
}

var diffOpts = []cmp.Option{
	cmp.Comparer(func(x, y big.Rat) bool {
		return x.String() == y.String()
	}),
	cmp.Comparer(func(x, y big.Int) bool {
		return x.String() == y.String()
	}),
	cmp.AllowUnexported(
		nullLit{},
		boolLit{},
		stringLit{},
		bytesLit{},
		numLit{},
	),
	cmpopts.IgnoreUnexported(
		bottom{},
		baseValue{},
		baseValue{},
	),
}

var (
	nullSentinel  = &nullLit{}
	trueSentinel  = &boolLit{b: true}
	falseSentinel = &boolLit{b: false}
)

func TestLiterals(t *testing.T) {
	t.Skip()
	mkMul := func(x int64, m literal.Multiplier, base int) *numLit {
		return newInt(testBase, m).setInt64(x)
	}
	hk := newInt(testBase, 0).setInt64(100000)
	testCases := []struct {
		lit  string
		node value
	}{
		{"0", mkInt(0)},
		{"null", nullSentinel},
		{"true", trueSentinel},
		{"false", falseSentinel},
		{"fls", &bottom{}},
		{`"foo"`, &stringLit{str: "foo"}},
		{`#"foo"#`, &stringLit{str: "foo"}},
		{`#""foo"#`, &stringLit{str: `"foo`}},
		{`#" ""#`, &stringLit{str: ` "`}},
		{`"\"foo\""`, &stringLit{str: `"foo"`}},
		{`"foo\u0032"`, &stringLit{str: `foo2`}},
		{`"foo\U00000033"`, &stringLit{str: `foo3`}},
		{`"foo\U0001f499"`, &stringLit{str: `foo💙`}},
		{`"\a\b\f\n\r\t\v"`, &stringLit{str: "\a\b\f\n\r\t\v"}},
		{`"""
		"""`, &stringLit{str: ""}},
		{`"""
			abc
			"""`, &stringLit{str: "abc"}},
		{`"""
			abc
			def
			"""`, &stringLit{str: "abc\ndef"}},
		{`"""
			abc
				def
			"""`, &stringLit{str: "abc\n\tdef"}},
		{`'\xff'`, &bytesLit{b: []byte("\xff")}},
		{"1", mkInt(1)},
		{"100_000", hk},
		{"1.", mkFloat("1")},
		{"0.0", mkFloat("0.0")},
		{".0", mkFloat(".0")},
		{"012.34", mkFloat("012.34")},
		{".01", mkFloat(".01")},
		{".01e2", mkFloat("1")},
		{"0.", mkFloat("0.")},
		{"1K", mkMul(1000, literal.K, 10)},
		{".5K", mkMul(500, literal.K, 10)},
		{"1Mi", mkMul(1024*1024, literal.Mi, 10)},
		{"1.5Mi", mkMul((1024+512)*1024, literal.Mi, 10)},
		{"1.3Mi", &bottom{}}, // Cannot be accurately represented.
		{"1.3G", mkMul(1300000000, literal.G, 10)},
		{"1.3e+20", mkFloat("1.3e+20")},
		{"1.3e20", mkFloat("1.3e+20")},
		{"1.3e-5", mkFloat("1.3e-5")},
		{".3e-1", mkFloat("0.3e-1")},
		{"0e-5", mkFloat("0e-5")},
		{"0E-5", mkFloat("0e-5")},
		{"5e-5", mkFloat("5e-5")},
		{"5E-5", mkFloat("5e-5")},
		{"0x1234", mkMul(0x1234, 0, 16)},
		{"0xABCD", mkMul(0xABCD, 0, 16)},
		{"-0xABCD", mkMul(0xABCD, 0, 16)},
		{"0b11001000", mkMul(0xc8, 0, 2)},
		{"0b1", mkMul(1, 0, 2)},
		{"0o755", mkMul(0755, 0, 8)},
		{"0755", mkMul(0755, 0, 8)},
	}
	p := litParser{
		ctx: &context{Context: &apd.BaseContext},
	}
	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d/%+q", i, tc.lit), func(t *testing.T) {
			got := p.parse(&ast.BasicLit{Value: tc.lit})
			if !cmp.Equal(got, tc.node, diffOpts...) {
				t.Error(cmp.Diff(got, tc.node, diffOpts...))
				t.Errorf("%#v, %#v\n", got, tc.node)
			}
		})
	}
}

func TestLiteralErrors(t *testing.T) {
	testCases := []struct {
		kind token.Token
		lit  string
	}{
		{token.STRING, `"foo\u"`},
		{token.STRING, `"foo\u003"`},
		{token.STRING, `"foo\U1234567"`},
		{token.STRING, `"foo\U12345678"`},
		{token.STRING, `"foo\Ug"`},
		{token.STRING, `"\xff"`},
		// not allowed in string literal, only binary
		{token.STRING, `"foo\x00"`},
		{token.INT, `0x`},
		{token.INT, `0o`},
		{token.INT, `0b`},
		{token.INT, `0_`},
		{token.INT, "0128"},
		{token.STRING, ``},
		{token.STRING, `"`},
		{token.STRING, `"a`},
		// wrong indentation
		{token.STRING, `"""
			abc
		def
			"""`},
		// non-matching quotes
		{token.STRING, `"""
			abc
			'''`},
		{token.STRING, `"""
			abc
			"`},
		{token.STRING, `"abc \( foo "`},
	}
	p := litParser{
		ctx: &context{Context: &apd.BaseContext},
	}
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%+q", tc.lit), func(t *testing.T) {
			got := p.parse(&ast.BasicLit{Kind: tc.kind, Value: tc.lit})
			if _, ok := got.(*bottom); !ok {
				t.Fatalf("expected error but found none")
			}
		})
	}
}
