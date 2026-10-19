// Copyright 2023 CUE Authors
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

package tdtest_test

import (
	"strings"
	"testing"

	"cuelang.org/go/internal/tdtest"
)

// TODO: write a proper test

// NOTE: for debugging purposes. Do not remove.
func TestX(t *testing.T) {
	t.Skip()

	type testCase struct {
		name string
		want string
	}
	_, cases := 1, []testCase{{
		name: "foo",
		want: `foo`,
	}}

	tdtest.Run(t, cases, func(t *tdtest.T, tc *testCase) {
		t.Update(true)
		t.Equal("actual", tc.want)
	})
}

// TestEqualUnnamedField checks that a mismatch found by an Equal call which
// cannot name its field, such as one outside the test function or in a test
// which cannot be analyzed, is reported with its values. Each case runs a
// test which fails, in a subprocess, and checks its output.
func TestEqualUnnamedField(t *testing.T) {
	tests := []struct {
		test string
		want string
	}{
		// Equal panics, as the call has no field name.
		{"TestOutsideHelper", "invalid memory address or nil pointer dereference"},
		// The test is only looked up in the file of the Equal call.
		{"TestOutsideHelperOtherFile", `could not find test "TestOutsideHelperOtherFile" in file`},
		// Updating panics, as the call has no field name.
		{"TestOutsideHelperUpdate", "invalid memory address or nil pointer dereference"},
		// Finding the Run call panics, as its function is not a literal.
		{"TestOutsideRunFunc", "interface conversion: ast.Expr is *ast.Ident, not *ast.FuncLit"},
		// Finding the Run call panics, as its function is not a literal.
		{"TestOutsideRunFuncType", "interface conversion: ast.Expr is *ast.Ident, not *ast.FuncLit"},
		{"TestOutsideDuplicateName", "unexpected value for field want:\ngot:  bar;\nwant: foo"},
		// Failing to analyze these tests fails their parent test from the
		// subtest, so the mismatch is not reported.
		{"TestNonFieldEqual", "subtest may have called FailNow on a parent test"},
		{"TestInlineTableUpdate", "subtest may have called FailNow on a parent test"},
	}
	for _, test := range tests {
		t.Run(test.test, func(t *testing.T) {
			t.Parallel()
			if out := tdtest.RunFailing(t, test.test); !strings.Contains(out, test.want) {
				t.Errorf("output does not contain %q:\n%s", test.want, out)
			}
		})
	}
}

type outsideCase struct {
	want string
}

var outsideCases = []outsideCase{{want: "foo"}}

func checkOutsideCase(t *tdtest.T, tc *outsideCase) {
	t.Equal("bar", tc.want)
}

func TestOutsideHelper(t *testing.T) {
	tdtest.SkipUnlessFailing(t)
	tdtest.Run(t, outsideCases, func(t *tdtest.T, tc *outsideCase) {
		checkOutsideCase(t, tc)
	})
}

func TestOutsideHelperOtherFile(t *testing.T) {
	tdtest.SkipUnlessFailing(t)
	tdtest.Run(t, outsideCases, func(t *tdtest.T, tc *outsideCase) {
		tdtest.CheckOutsideOtherFile(t, tc.want)
	})
}

func TestOutsideHelperUpdate(t *testing.T) {
	tdtest.SkipUnlessFailing(t)
	tdtest.Run(t, outsideCases, func(t *tdtest.T, tc *outsideCase) {
		t.Update(true)
		checkOutsideCase(t, tc)
	})
}

func TestOutsideRunFunc(t *testing.T) {
	tdtest.SkipUnlessFailing(t)
	tdtest.Run(t, outsideCases, checkOutsideCase)
}

type outsideFunc func(t *tdtest.T, tc *outsideCase)

func TestOutsideRunFuncType(t *testing.T) {
	tdtest.SkipUnlessFailing(t)
	var fn outsideFunc = checkOutsideCase
	tdtest.Run(t, outsideCases, fn)
}

// TestOutsideDuplicateName shares its name with a test in package tdtest,
// whose declaration must not be mistaken for this one.
func TestOutsideDuplicateName(t *testing.T) {
	tdtest.SkipUnlessFailing(t)
	tdtest.Run(t, outsideCases, func(t *tdtest.T, tc *outsideCase) {
		t.Equal("bar", tc.want)
	})
}

// TestNonFieldEqual compares a value which is not a test case field,
// so its test cannot be analyzed.
func TestNonFieldEqual(t *testing.T) {
	tdtest.SkipUnlessFailing(t)
	tdtest.Run(t, outsideCases, func(t *tdtest.T, tc *outsideCase) {
		t.Equal(1, 3)
	})
}

// TestInlineTableUpdate passes Run a table which is not a variable,
// so its test cannot be analyzed.
func TestInlineTableUpdate(t *testing.T) {
	tdtest.SkipUnlessFailing(t)
	tdtest.Run(t, []outsideCase{{want: "foo"}}, func(t *tdtest.T, tc *outsideCase) {
		t.Update(true)
		t.Equal("bar", tc.want)
	})
}
