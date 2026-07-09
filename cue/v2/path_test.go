// Copyright 2026 The CUE Authors
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

package cue_test

import (
	"testing"

	cue "cuelang.org/go/cue/v2"
	"github.com/go-quicktest/qt"
)

func TestParsePathRoundTrip(t *testing.T) {
	for _, s := range []string{
		"",
		"a",
		"a.b.c",
		"a[2].c",
		"[2].c",
		`"x-y".z`,
		"#Def.a",
		`a."b c"`,
	} {
		p := cue.ParsePath(s)
		qt.Assert(t, qt.IsNil(p.Err()), qt.Commentf("path %q", s))
		qt.Assert(t, qt.Equals(p.String(), s), qt.Commentf("path %q", s))
	}
}

func TestParsePathErrors(t *testing.T) {
	for _, tc := range []struct {
		path    string
		wantErr string
	}{
		{"a[b]", ".*non-constant expression.*"},
		{"_hidden", ".*hidden.*"},
		{"a.b _|_", ".*expected .EOF.*"},
		{"1+2", ".*invalid label.*"},
	} {
		p := cue.ParsePath(tc.path)
		qt.Assert(t, qt.IsNotNil(p.Err()), qt.Commentf("path %q", tc.path))
		qt.Assert(t, qt.ErrorMatches(p.Err(), tc.wantErr), qt.Commentf("path %q", tc.path))
		qt.Assert(t, qt.Equals(p.String(), "_|_"))
	}
}

func TestMakePath(t *testing.T) {
	p := cue.MakePath(cue.Str("a"), cue.Index(2), cue.Str("b-c"), cue.Def("D"))
	qt.Assert(t, qt.IsNil(p.Err()))
	qt.Assert(t, qt.Equals(p.String(), `a[2]."b-c".#D`))

	sels := p.Selectors()
	qt.Assert(t, qt.Equals(len(sels), 4))
	qt.Assert(t, qt.Equals(sels[0].Unquoted(), "a"))
	qt.Assert(t, qt.IsTrue(sels[0].IsString()))
	qt.Assert(t, qt.Equals(sels[1].Index(), 2))
	qt.Assert(t, qt.IsTrue(sels[3].IsDefinition()))

	// Def adds the leading # as needed.
	qt.Assert(t, qt.Equals(cue.Def("#E").String(), "#E"))

	// A negative index yields an invalid selector, reported by Err.
	bad := cue.MakePath(cue.Index(-1))
	qt.Assert(t, qt.IsNotNil(bad.Err()))
}

func TestSelectorPanics(t *testing.T) {
	qt.Assert(t, qt.PanicMatches(func() {
		cue.Index(2).Unquoted()
	}, ".*non-string label.*"))
	qt.Assert(t, qt.PanicMatches(func() {
		cue.Str("a").Index()
	}, ".*non-index selector.*"))
	qt.Assert(t, qt.PanicMatches(func() {
		cue.Def("not an ident!")
	}, ".*invalid definition.*"))
}
