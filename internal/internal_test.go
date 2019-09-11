// Copyright 2019 CUE Authors
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

package internal_test

import (
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/internal"
	"github.com/stretchr/testify/assert"
)

func TestLabelName(t *testing.T) {
	testCases := []struct {
		in  ast.Label
		out string
		ok  bool
	}{{
		in:  ast.NewString("foo-bar"),
		out: "foo-bar",
		ok:  true,
	}, {
		in:  ast.NewString("foo bar"),
		out: "foo bar",
		ok:  true,
	}, {
		in:  &ast.Ident{Name: "`foo-bar`"},
		out: "foo-bar",
		ok:  true,
	}, {
		in:  &ast.Ident{Name: "`foo-bar\x00`"},
		out: "",
		ok:  false,
	}}
	for _, tc := range testCases {
		b, _ := format.Node(tc.in)
		t.Run(string(b), func(t *testing.T) {
			str, ok := internal.LabelName(tc.in)
			assert.Equal(t, tc.out, str)
			assert.Equal(t, tc.ok, ok)
		})
	}
}
