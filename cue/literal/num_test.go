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

package literal

import (
	"fmt"
	"math/big"
	"strconv"
	"testing"

	"cuelang.org/go/cue/token"
	"github.com/cockroachdb/apd/v3"
	"github.com/go-quicktest/qt"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

func mkInt(i int) NumInfo {
	return NumInfo{
		base: 10,
		neg:  i < 0,
		buf:  []byte(strconv.Itoa(i)),
	}
}

func mkFloat(a string) NumInfo {
	return NumInfo{
		base:    10,
		buf:     []byte(a),
		neg:     a[0] == '-',
		isFloat: true,
	}
}

func mkMul(i string, m Multiplier, base byte) NumInfo {
	return NumInfo{
		base: base,
		mul:  m,
		neg:  i[0] == '-',
		buf:  []byte(i),
	}
}

func TestNumbers(t *testing.T) {
	// hk := newInt(testBase, newRepresentation(0, 10, true)).setInt64(100000)
	testCases := []struct {
		lit  string
		norm string
		n    NumInfo
	}{
		{"0", "0", mkInt(0)},
		{"1", "1", mkInt(1)},
		{"+1", "1", mkInt(1)},
		{"-1", "-1", mkInt(-1)},
		{"100_000", "100000", NumInfo{UseSep: true, base: 10, buf: []byte("100000")}},
		{"1.", "1.", mkFloat("1.")},
		{"0.", "0.", mkFloat("0.")},
		{".0", "0.0", mkFloat("0.0")},
		{"012.34", "12.34", mkFloat("12.34")},
		{".01", "0.01", mkFloat("0.01")},
		{".01e2", "0.01e2", mkFloat("0.01e2")},
		{"0.", "0.", mkFloat("0.")},
		{"1K", "1000", mkMul("1", K, 10)},
		{".5K", "500", mkMul("0.5", K, 10)},
		{"1Mi", "1048576", mkMul("1", Mi, 10)},
		{"1.5Mi", "1572864", mkMul("1.5", Mi, 10)},
		// {"1.3Mi", &bottom{}}, // Cannot be accurately represented.
		{"1.3G", "1300000000", mkMul("1.3", G, 10)},
		// A multiplier must not round a literal wider than 34 digits.
		{"123456789012345678901234567890123456789K", "123456789012345678901234567890123456789000", mkMul("123456789012345678901234567890123456789", K, 10)},
		{"1.3e+20", "1.3e+20", mkFloat("1.3e+20")},
		{"1.3e20", "1.3e20", mkFloat("1.3e20")},
		{"1.3e-5", "1.3e-5", mkFloat("1.3e-5")},
		{".3e-1", "0.3e-1", mkFloat("0.3e-1")},
		{"0e-5", "0e-5", mkFloat("0e-5")},
		{"0E-5", "0e-5", mkFloat("0e-5")},
		{"5e-5", "5e-5", mkFloat("5e-5")},
		{"5E-5", "5e-5", mkFloat("5e-5")},
		{"0x1234", "4660", mkMul("1234", 0, 16)},
		{"0xABCD", "43981", mkMul("ABCD", 0, 16)},
		{"-0xABCD", "-43981", mkMul("-ABCD", 0, 16)},
		{"0b11001000", "200", mkMul("11001000", 0, 2)},
		{"0b1", "1", mkMul("1", 0, 2)},
		{"0o755", "493", mkMul("755", 0, 8)},
	}
	n := NumInfo{}
	for i, tc := range testCases {
		t.Run(fmt.Sprintf("%d/%+q", i, tc.lit), func(t *testing.T) {
			if err := ParseNum(tc.lit, &n); err != nil {
				t.Fatal(err)
			}
			n.src = ""
			n.p = 0
			n.ch = 0
			qt.Assert(t, qt.CmpEquals(n, tc.n, diffOpts...))
			qt.Assert(t, qt.Equals(n.String(), tc.norm))
		})
	}

	// dec is the result of [NumInfo.Decimal], or its error.
	decCases := []struct {
		lit string
		dec string
	}{
		{"1.5Mi", "1572864"},
		{"0x1234", "4660"},
		{"1.3e-5", "0.000013"},
		{"1e100000", "1E+100000"},
		{"1e-100000", "1E-100000"},
		{"123e99998", "1.23E+100000"},
		{"0.1e-99999", "1E-100000"},
		// A zero is zero with any exponent.
		{"0e100001", "0"},
		// The exponent of the last digit is also limited, as arithmetic fails
		// beyond it.
		{"1.5e-100000", `exponent out of range in number "1.5e-100000"`},
		// An exponent out of range must not result in another number.
		{"1e100001", `exponent out of range in number "1e100001"`},
		{"1e-100020", `exponent out of range in number "1e-100020"`},
		{"123e99999", `exponent out of range in number "123e99999"`},
		{"0.001e100003", `exponent out of range in number "0.001e100003"`},
		{"1e2147483648", `exponent out of range in number "1e2147483648"`},
	}
	for _, tc := range decCases {
		t.Run("Decimal/"+tc.lit, func(t *testing.T) {
			qt.Assert(t, qt.IsNil(ParseNum(tc.lit, &n)))
			var d apd.Decimal
			var got string
			if err := n.Decimal(&d); err != nil {
				got = err.Error()
			} else {
				got = d.String()
			}
			qt.Assert(t, qt.Equals(got, tc.dec))
		})
	}
}

var diffOpts = []cmp.Option{
	cmp.Comparer(func(x, y big.Rat) bool {
		return x.String() == y.String()
	}),
	cmp.Comparer(func(x, y big.Int) bool {
		return x.String() == y.String()
	}),
	cmp.AllowUnexported(
		NumInfo{},
	),
	cmpopts.IgnoreUnexported(
		token.Pos{},
	),
	cmpopts.EquateEmpty(),
}

func TestNumErrors(t *testing.T) {
	testCases := []string{
		`0x`,
		`0o`,
		`0b`,
		`0_`,
		`1__0`,
		`0x1__0`,
		`0b1__0`,
		`0o1__0`,
		"0000",
		"00",
		"0128",
		"0A",
		"0Z",
		"1A",
		"99Z",
		"0xFG",
		"e+100",
		".p",
		``,
		`"`,
		`"a`,
		`23.34e`,
		`23.34e33pp`,
		"0K0",
		"1K0",
		"1Ki0",
		"1.5M0",
		"1Kx",
		"0\x00",
		"1\x00",
		"\x000",
	}
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%+q", tc), func(t *testing.T) {
			n := &NumInfo{}
			err := ParseNum(tc, n)
			if err == nil {
				t.Fatalf("expected error but found none")
			}
		})
	}
}
