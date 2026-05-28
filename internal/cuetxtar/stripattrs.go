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
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
)

// StripTestAttrs returns src reformatted with every @test(...) attribute
// removed.  Both forms are stripped:
//
//   - field attributes: `field: expr @test(...)`
//   - decl attributes inside a struct or file: `@test(...)` as a top-level
//     declaration
//
// Other attributes are preserved.  Use this when an unrelated downstream
// consumer (e.g. the compile golden tests) mirrors CUE source from
// cue/testdata and would otherwise see incidental churn whenever a @test
// directive is added, updated, or removed.
func StripTestAttrs(src []byte) ([]byte, error) {
	// Fast path: a file with no @test substring cannot contain @test(...)
	// attributes; skipping parse + format preserves the source bytes
	// (including incidental whitespace) for files that have nothing to
	// strip.  A false positive (e.g. "@test" inside a comment or string)
	// only costs an extra round trip; it does not change correctness.
	if !bytes.Contains(src, []byte("@test")) {
		return src, nil
	}

	f, err := parser.ParseFile("", src, parser.ParseComments)
	if err != nil {
		return src, err
	}

	astutil.Apply(f, func(c astutil.Cursor) bool {
		switch x := c.Node().(type) {
		case *ast.Field:
			// Field-level attributes are kept on the Field itself, not as
			// list entries on the parent declsCursor, so we filter the slice
			// in place rather than relying on Cursor.Delete.
			x.Attrs = filterTestAttrs(x.Attrs)
		case *ast.Attribute:
			// Decl-level attributes (inside *ast.File or *ast.StructLit) can
			// be removed via the cursor.  Attribute nodes encountered as part
			// of a Field's Attrs walk were already filtered above.
			switch c.Parent().Node().(type) {
			case *ast.File, *ast.StructLit:
				if name, _ := x.Split(); name == "test" {
					c.Delete()
				}
			}
		}
		return true
	}, nil)

	return format.Node(f)
}

func filterTestAttrs(attrs []*ast.Attribute) []*ast.Attribute {
	out := attrs[:0]
	for _, a := range attrs {
		if name, _ := a.Split(); name == "test" {
			continue
		}
		out = append(out, a)
	}
	return out
}
