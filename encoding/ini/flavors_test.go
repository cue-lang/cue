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

package ini_test

import (
	"bytes"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/encoding/ini"
	"cuelang.org/go/internal/cuetxtar"
)

// TestFlavors decodes in.ini of each archive in testdata/flavors under the
// zero Config into out/decode/default. An archive tagged #crlf is decoded with
// CRLF line endings, which the archive does not store so that git cannot
// convert them.
//
// A file the zero Config accepts must also decode with every value equal to
// its trimmed source text.
func TestFlavors(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root: "testdata/flavors",
		Name: "decode",
	}
	test.Run(t, func(t *cuetxtar.Test) {
		var data []byte
		for _, f := range t.Archive.Files {
			if f.Name == "in.ini" {
				data = f.Data
			}
		}
		qt.Assert(t, qt.IsNotNil(data))
		if t.HasTag("crlf") {
			data = bytes.ReplaceAll(data, []byte("\n"), []byte("\r\n"))
		}

		expr := decode(t, t.Writer("default"), data, ini.Config{})
		if expr != nil {
			checkTrimmedValues(t, expr, data)
		}
	})
}

// decode decodes data under cfg and writes the formatted CUE, or the error
// with its positions, to w. It returns the decoded expression, or nil on error.
func decode(t *cuetxtar.Test, w io.Writer, data []byte, cfg ini.Config) ast.Expr {
	expr, err := ini.NewDecoder("in.ini", bytes.NewReader(data), cfg).Decode()
	if err != nil {
		fmt.Fprint(w, errors.Details(err, nil))
		return nil
	}
	file, err := astutil.ToFile(expr)
	qt.Assert(t, qt.IsNil(err))
	out, err := format.Node(file)
	qt.Assert(t, qt.IsNil(err))
	w.Write(out)
	return expr
}

// checkTrimmedValues checks that expr holds exactly the properties of data,
// each with its trimmed source text as the value.
func checkTrimmedValues(t *cuetxtar.Test, expr ast.Expr, data []byte) {
	got := flattenStrings(t, expr, nil, map[string]string{})
	var section string
	properties := 0
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line[0] == ';' || line[0] == '#' {
			continue
		}
		if line[0] == '[' {
			section = strings.TrimSpace(strings.TrimSuffix(line[1:], "]"))
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		qt.Assert(t, qt.IsTrue(ok), qt.Commentf("line %q", line))
		path := strings.TrimSpace(key)
		if section != "" {
			path = section + "\x00" + path
		}
		properties++
		qt.Check(t, qt.Equals(got[path], strings.TrimSpace(value)), qt.Commentf("line %q", line))
	}
	qt.Assert(t, qt.Equals(len(got), properties))
}

// flattenStrings collects every string-valued field of expr into out, keyed by
// its path with the segments separated by a NUL byte.
func flattenStrings(t testing.TB, expr ast.Expr, path []string, out map[string]string) map[string]string {
	t.Helper()
	for _, elt := range expr.(*ast.StructLit).Elts {
		field := elt.(*ast.Field)
		name, _, err := ast.LabelName(field.Label)
		qt.Assert(t, qt.IsNil(err))
		path := append(path, name)
		if _, ok := field.Value.(*ast.StructLit); ok {
			flattenStrings(t, field.Value, path, out)
			continue
		}
		lit := field.Value.(*ast.BasicLit)
		s, err := literal.Unquote(lit.Value)
		qt.Assert(t, qt.IsNil(err))
		out[strings.Join(path, "\x00")] = s
	}
	return out
}
