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

package cuetxtar

import (
	"bytes"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
)

// StripTestAttrs returns src with every @test(...) attribute removed.  Both
// forms are stripped:
//
//   - field attributes: `field: expr @test(...)`
//   - decl attributes inside a struct or file: `@test(...)` as a top-level
//     declaration
//
// All other source bytes are preserved verbatim. We deliberately avoid
// reformatting via cue/format so that any CUE syntax that matters for a test
// is left untouched; the parser is used only to locate the @test attributes,
// whose byte ranges (plus surrounding whitespace) are cut out. Use this when
// an unrelated downstream consumer (e.g. the compile golden tests) mirrors
// CUE source from cue/testdata and would otherwise see incidental churn
// whenever a @test directive is added, updated, or removed.
func StripTestAttrs(src []byte) ([]byte, error) {
	// Fast path: a file with no @test substring has nothing to strip. A false
	// positive (e.g. "@test" in a comment or string) only costs an extra
	// parse, since the walk below locates real attribute nodes only.
	if !bytes.Contains(src, []byte("@test")) {
		return src, nil
	}

	f, err := parser.ParseFile("", src, parser.ParseComments)
	if err != nil {
		return src, err
	}

	// ast.Walk visits both field-level attributes (Field.Attrs) and decl-level
	// attributes (in *ast.File or *ast.StructLit), so one walk covers both.
	var out []byte
	prev := 0
	ast.Walk(f, func(n ast.Node) bool {
		a, ok := n.(*ast.Attribute)
		if !ok || a.Name() != "test" {
			return true
		}
		start, end := a.Pos().Offset(), a.End().Offset()
		// Drop spaces and tabs before the attribute, so a field attribute
		// `field: expr @test(...)` leaves `field: expr` with no trailing space.
		start = prev + len(bytes.TrimRight(src[prev:start], " \t"))
		// If the attribute then occupies its own line, drop the trailing
		// newline too so a decl-level `@test(...)` line vanishes entirely.
		if (start == 0 || src[start-1] == '\n') && end < len(src) && src[end] == '\n' {
			end++
		}
		out = append(out, src[prev:start]...)
		prev = end
		return true
	}, nil)
	out = append(out, src[prev:]...)
	return out, nil
}
