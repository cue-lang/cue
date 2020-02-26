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
		{"0755", "493", mkMul("755", 0, 8)},
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
			if !cmp.Equal(n, tc.n, diffOpts...) {
				t.Error(cmp.Diff(n, tc.n, diffOpts...))
				t.Errorf("%#v, %#v\n", n, tc.n)
			}
			if n.String() != tc.norm {
				t.Errorf("got %v; want %v", n.String(), tc.norm)
			}
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
		"0128",
		"e+100",
		".p",
		``,
		`"`,
		`"a`,
		`23.34e`,
		`23.34e33pp`,
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
