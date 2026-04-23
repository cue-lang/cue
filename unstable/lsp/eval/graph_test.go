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

package eval_test

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/unstable/lsp/eval"
	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"
)

func TestGraph(t *testing.T) {
	graphTestCases{
		{
			name: "PackageAndFieldDocs",
			archive: `-- a.cue --
// mypkg is the test package.
package mypkg

// x documents the value 1.
x: 1

// y is a string field.
y: "hello"
`,
			wantAtPath: map[string]nodeWant{
				"": {
					children: []string{"x", "y"},
					decls: []declWant{
						{Key: "", Value: "File(a.cue)", Doc: ""},
						{Key: "mypkg @ a.cue:2", Value: "", Doc: "// mypkg is the test package."},
					},
				},
				"x": {
					children: []string{},
					decls: []declWant{
						{Key: "x @ a.cue:5", Value: "1", Doc: "// x documents the value 1."},
					},
				},
				"y": {
					children: []string{},
					decls: []declWant{
						{Key: "y @ a.cue:8", Value: `"hello"`, Doc: "// y is a string field."},
					},
				},
			},
		},

		{
			name: "MultiLineDocComments",
			archive: `-- a.cue --
package p

// x has a multi-line
// doc comment.
x: 42
`,
			wantAtPath: map[string]nodeWant{
				"x": {
					decls: []declWant{
						{Key: "x @ a.cue:5", Value: "42", Doc: "// x has a multi-line\n// doc comment."},
					},
				},
			},
		},

		{
			name: "MergedFieldsAcrossFiles",
			archive: `-- a.cue --
package p

// x in file a
x: 1
-- b.cue --
package p

// x in file b
x: 2

// only in b
y: "b"
`,
			wantAtPath: map[string]nodeWant{
				"": {
					children: []string{"x", "y"},
					decls: []declWant{
						{Key: "", Value: "File(a.cue)", Doc: ""},
						{Key: "", Value: "File(b.cue)", Doc: ""},
						{Key: "p @ a.cue:1", Value: "", Doc: ""},
						{Key: "p @ b.cue:1", Value: "", Doc: ""},
					},
				},
				"x": {
					decls: []declWant{
						{Key: "x @ a.cue:4", Value: "1", Doc: "// x in file a"},
						{Key: "x @ b.cue:4", Value: "2", Doc: "// x in file b"},
					},
				},
				"y": {
					decls: []declWant{
						{Key: "y @ b.cue:7", Value: `"b"`, Doc: "// only in b"},
					},
				},
			},
		},

		{
			name: "NestedFields",
			archive: `-- a.cue --
package p

// outer is a struct.
outer: {
	// inner is a number.
	inner: 42
}
`,
			wantAtPath: map[string]nodeWant{
				"": {
					children: []string{"outer"},
				},
				"outer": {
					children: []string{"inner"},
					decls: []declWant{
						{
							Key:   "outer @ a.cue:4",
							Value: "{\n\t// inner is a number.\n\tinner: 42\n}",
							Doc:   "// outer is a struct.",
						},
					},
				},
				"outer.inner": {
					children: []string{},
					decls: []declWant{
						{Key: "inner @ a.cue:6", Value: "42", Doc: "// inner is a number."},
					},
				},
			},
		},

		{
			name: "ReferenceExpandsChildren",
			archive: `-- a.cue --
package p

x: {
	a: 1
	b: 2
}
y: x
`,
			wantAtPath: map[string]nodeWant{
				"y": {
					// y resolves to x, so its children should be x's children.
					children: []string{"a", "b"},
					decls: []declWant{
						{Key: "y @ a.cue:7", Value: "x", Doc: ""},
					},
				},
				"y.a": {
					decls: []declWant{
						{Key: "a @ a.cue:4", Value: "1", Doc: ""},
					},
				},
			},
		},

		{
			name: "FieldWithoutDoc",
			archive: `-- a.cue --
package p

x: 1
`,
			wantAtPath: map[string]nodeWant{
				"x": {
					children: []string{},
					decls: []declWant{
						{Key: "x @ a.cue:3", Value: "1", Doc: ""},
					},
				},
			},
		},
	}.run(t)
}

type graphTestCase struct {
	name    string
	archive string
	// wantAtPath maps dotted paths ("" for root) to expectations about
	// the node at that path. Only the populated fields of each
	// [nodeWant] are checked.
	wantAtPath map[string]nodeWant
}

type nodeWant struct {
	// children, if non-nil, is the expected sorted set of names
	// returned by Children() at this node. An empty (non-nil) slice
	// asserts that the node has no children.
	children []string
	// decls, if non-nil, is the expected set of [eval.Decl] entries
	// yielded by Decls() at this node, sorted by (Key, Value, Doc).
	decls []declWant
}

// declWant captures the rendered form of an [eval.Decl] for
// comparison.  A field set to the empty string asserts that the
// corresponding accessor on the Decl returns nil. Fields are exported
// so that go-cmp (used by qt.DeepEquals) can inspect them.
type declWant struct {
	// Key is the rendered text of [eval.Decl.Key], formatted as
	// "<keyText> @ <filename>:<line>", or "" when the Decl's key is
	// nil.
	Key string
	// Value is the rendered text of [eval.Decl.Value]. *ast.File nodes
	// render as "File(<filename>)"; other nodes use [format.Node]; nil
	// renders as "".
	Value string
	// Doc is the joined text of [eval.Decl.DocComments], with comment
	// groups separated by newlines.
	Doc string
}

type graphTestCases []graphTestCase

func (tcs graphTestCases) run(t *testing.T) {
	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			ar := txtar.Parse([]byte(tc.archive))
			qt.Assert(t, qt.IsTrue(len(ar.Files) > 0))

			var files []*ast.File
			for _, fh := range ar.Files {
				f, err := parser.ParseFile(fh.Name, fh.Data, parser.ParseComments)
				qt.Assert(t, qt.IsNil(err))
				f.Pos().File().SetContent(fh.Data)
				files = append(files, f)
			}

			cfg := eval.Config{IP: ast.ImportPath{Path: "test"}.Canonical()}
			ev := eval.New(cfg, files...)
			root := ev.Root()

			for path, want := range tc.wantAtPath {
				node := navigateGraph(root, path)
				qt.Assert(t, qt.IsNotNil(node), qt.Commentf("path %q", path))

				if want.children != nil {
					got := slices.Sorted(maps.Keys(node.Children()))
					if got == nil {
						got = []string{}
					}
					qt.Check(t, qt.DeepEquals(got, want.children), qt.Commentf("path %q: Children()", path))
				}
				if want.decls != nil {
					got := renderDecls(node)
					qt.Check(t, qt.DeepEquals(got, want.decls), qt.Commentf("path %q: Decls()", path))
				}
			}
		})
	}
}

func navigateGraph(root *eval.Node, path string) *eval.Node {
	if path == "" {
		return root
	}
	n := root
	for _, p := range strings.Split(path, ".") {
		n = n.Children()[p]
		if n == nil {
			return nil
		}
	}
	return n
}

func renderDecls(n *eval.Node) []declWant {
	var out []declWant
	for d := range n.Decls() {
		out = append(out, declWant{
			Key:   renderKeyNode(d.Key()),
			Value: renderValueNode(d.Value()),
			Doc:   joinDocComments(d.DocComments()),
		})
	}
	slices.SortFunc(out, func(a, b declWant) int {
		return cmp.Or(
			cmp.Compare(a.Key, b.Key),
			cmp.Compare(a.Value, b.Value),
			cmp.Compare(a.Doc, b.Doc),
		)
	})
	return out
}

func renderKeyNode(n ast.Node) string {
	if n == nil {
		return ""
	}
	var text string
	switch k := n.(type) {
	case *ast.Ident:
		text = k.Name
	case *ast.BasicLit:
		text = k.Value
	default:
		text = fmt.Sprintf("%T", n)
	}
	pos := n.Pos().Position()
	return fmt.Sprintf("%s @ %s:%d", text, pos.Filename, pos.Line)
}

func renderValueNode(n ast.Node) string {
	if n == nil {
		return ""
	}
	if f, ok := n.(*ast.File); ok {
		return fmt.Sprintf("File(%s)", f.Filename)
	}
	b, err := format.Node(n)
	if err != nil {
		return fmt.Sprintf("<err:%v>", err)
	}
	return strings.TrimSpace(string(b))
}

func joinDocComments(groups []*ast.CommentGroup) string {
	var b strings.Builder
	for i, g := range groups {
		if i > 0 {
			b.WriteByte('\n')
		}
		for j, c := range g.List {
			if j > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(c.Text)
		}
	}
	return b.String()
}
