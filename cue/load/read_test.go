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

package load

import (
	"io"
	"strings"
	"testing"

	"cuelang.org/go/cue/errors"
)

const quote = "`"

type readTest struct {
	// Test input contains ℙ where readImports should stop.
	in  string
	err string
}

var readImportsTests = []readTest{
	{
		`package p`,
		"",
	},
	{
		`package p; import "x"`,
		"",
	},
	{
		`package p; import . "x"`,
		"",
	},
	{
		`package p; import "x";ℙvar x = 1`,
		"",
	},
	{
		`package p
		
		// comment
		
		import "x"
		import _ "x"
		import a "x"

		import (
			"x"
			_ "x"
			a "x" // comment
			` + quote + `x` + quote + `
			_ ` + quote + `x` + quote + `
			a ` + quote + `x` + quote + `
		)
		import (
		)
		import ()
		import()import()import()
		import();import();import()

		ℙvar x = 1
		`,
		"",
	},
}

var readCommentsTests = []readTest{
	{
		`ℙpackage p`,
		"",
	},
	{
		`ℙpackage p; import "x"`,
		"",
	},
	{
		`ℙpackage p; import . "x"`,
		"",
	},
	{
		`// foo

		// asdf
		ℙHello, world`,
		"",
	},
}

func testRead(t *testing.T, tests []readTest, read func(io.Reader) ([]byte, errors.Error)) {
	for i, tt := range tests {
		var in, testOut string
		j := strings.Index(tt.in, "ℙ")
		if j < 0 {
			in = tt.in
			testOut = tt.in
		} else {
			in = tt.in[:j] + tt.in[j+len("ℙ"):]
			testOut = tt.in[:j]
		}
		r := strings.NewReader(in)
		buf, err := read(r)
		if err != nil {
			if tt.err == "" {
				t.Errorf("#%d: err=%q, expected success (%q)", i, err, string(buf))
				continue
			}
			if !strings.Contains(err.Error(), tt.err) {
				t.Errorf("#%d: err=%q, expected %q", i, err, tt.err)
				continue
			}
			continue
		}
		if tt.err != "" {
			t.Errorf("#%d: success, expected %q", i, tt.err)
			continue
		}

		out := string(buf)
		if out != testOut {
			t.Errorf("#%d: wrong output:\nhave %q\nwant %q\n", i, out, testOut)
		}
	}
}

func TestReadImports(t *testing.T) {
	testRead(t, readImportsTests, func(r io.Reader) ([]byte, errors.Error) {
		return readImports(r, true, nil)
	})
}

func TestReadComments(t *testing.T) {
	testRead(t, readCommentsTests, readComments)
}

var readFailuresTests = []readTest{
	{
		`package`,
		"syntax error",
	},
	{
		"package p\n\x00\nimport `math`\n",
		"unexpected NUL in input",
	},
	{
		`package p; import`,
		"syntax error",
	},
	{
		`package p; import "`,
		"syntax error",
	},
	{
		"package p; import ` \n\n",
		"syntax error",
	},
	{
		`package p; import "x`,
		"syntax error",
	},
	{
		`package p; import _`,
		"syntax error",
	},
	{
		`package p; import _ "`,
		"syntax error",
	},
	{
		`package p; import _ "x`,
		"syntax error",
	},
	{
		`package p; import .`,
		"syntax error",
	},
	{
		`package p; import . "`,
		"syntax error",
	},
	{
		`package p; import . "x`,
		"syntax error",
	},
	{
		`package p; import (`,
		"syntax error",
	},
	{
		`package p; import ("`,
		"syntax error",
	},
	{
		`package p; import ("x`,
		"syntax error",
	},
	{
		`package p; import ("x"`,
		"syntax error",
	},
}

func TestReadFailures(t *testing.T) {
	// Errors should be reported (true arg to readImports).
	testRead(t, readFailuresTests, func(r io.Reader) ([]byte, errors.Error) {
		return readImports(r, true, nil)
	})
}

func TestReadFailuresIgnored(t *testing.T) {
	// Syntax errors should not be reported (false arg to readImports).
	// Instead, entire file should be the output and no error.
	// Convert tests not to return syntax errors.
	tests := make([]readTest, len(readFailuresTests))
	copy(tests, readFailuresTests)
	for i := range tests {
		tt := &tests[i]
		if !strings.Contains(tt.err, "NUL") {
			tt.err = ""
		}
	}
	testRead(t, tests, func(r io.Reader) ([]byte, errors.Error) {
		return readImports(r, false, nil)
	})
}
