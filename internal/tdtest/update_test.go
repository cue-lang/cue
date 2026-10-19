// Copyright 2026 CUE Authors
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

package tdtest

import (
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"golang.org/x/tools/go/packages"
)

// TestUpdateField checks the source that updating a field
// writes for each kind of scalar value.
func TestUpdateField(t *testing.T) {
	tests := []struct {
		value any
		want  string
	}{
		{"foo", "`foo`"},
		{true, "true"},
		{int(1), "1"},
		{int8(2), "2"},
		{int16(3), "3"},
		{int32(4), "4"},
		{int64(5), "5"},
		{uint(6), "6"},
		{uint8(7), "7"},
		{uint16(8), "8"},
		{uint32(9), "9"},
		{uint64(10), "10"},
		{uintptr(11), "11"},
	}
	for _, test := range tests {
		info := updateTableField(t, "[]testCase{{field: 0}}", test.value)

		var patched []string
		for _, expr := range info.patches {
			var sb strings.Builder
			if err := format.Node(&sb, info.testPkg.Fset, expr); err != nil {
				t.Fatal(err)
			}
			patched = append(patched, sb.String())
		}
		if len(patched) != 1 || patched[0] != test.want {
			t.Errorf("updating to %T(%v) wrote %q, want %q", test.value, test.value, patched, test.want)
		}
	}
}

// TestUpdateFieldPositional checks that updating a field of a table row
// without keys fails the test with an error. It runs in a subprocess, as
// the failure ends the test.
func TestUpdateFieldPositional(t *testing.T) {
	if os.Getenv(failingEnv) != "" {
		updateTableField(t, "[]testCase{{0}}", 1)
		return
	}
	out := RunFailing(t, t.Name())
	want := "1:13: elements must be key value pairs"
	if !strings.Contains(out, want) {
		t.Errorf("output does not contain %q:\n%s", want, out)
	}
}

// updateTableField updates the field named "field" of the only row of the
// given table source, returning the info holding the resulting patches.
func updateTableField(t *testing.T, table string, value any) *info {
	t.Helper()
	fset := token.NewFileSet()
	expr, err := parser.ParseExprFrom(fset, "", table, 0)
	if err != nil {
		t.Fatal(err)
	}
	info := &info{
		table:   expr.(*ast.CompositeLit),
		testPkg: &packages.Package{Fset: fset},
		patches: make(map[ast.Node]ast.Expr),
	}
	tt := &T{T: t}
	tt.updateField(info, &callInfo{fieldName: "field"}, value)
	return info
}

// failingEnv is set in the subprocesses started by [RunFailing].
const failingEnv = "CUE_TEST_TDTEST_FAILING"

// RunFailing runs the named test, which is expected to fail, in a
// subprocess, and returns its output with the indentation of each line
// removed. The test should start with [SkipUnlessFailing].
func RunFailing(t *testing.T, name string) string {
	t.Helper()
	args := []string{"-test.run=^" + name + "$"}
	if deadline, ok := t.Deadline(); ok {
		// Do not let the subprocess outlive this test.
		args = append(args, "-test.timeout="+time.Until(deadline).String())
	}
	cmd := exec.Command(os.Args[0], args...)
	cmd.Env = append(os.Environ(), failingEnv+"=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("test passed:\n%s", out)
	}
	lines := strings.Split(string(out), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimLeft(line, " \t")
	}
	return strings.Join(lines, "\n")
}

// SkipUnlessFailing skips a test which fails on purpose,
// unless it is run by [RunFailing].
func SkipUnlessFailing(t *testing.T) {
	t.Helper()
	if os.Getenv(failingEnv) == "" {
		t.Skip("run by RunFailing")
	}
}
