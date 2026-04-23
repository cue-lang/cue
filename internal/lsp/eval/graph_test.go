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
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/lsp/eval"
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
					docs: map[string]string{
						"mypkg @ a.cue:2": "// mypkg is the test package.",
					},
					values: []string{"File(a.cue)"},
				},
				"x": {
					children: []string{},
					docs: map[string]string{
						"x @ a.cue:5": "// x documents the value 1.",
					},
					values: []string{"1"},
				},
				"y": {
					children: []string{},
					docs: map[string]string{
						"y @ a.cue:8": "// y is a string field.",
					},
					values: []string{`"hello"`},
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
					docs: map[string]string{
						"x @ a.cue:5": "// x has a multi-line\n// doc comment.",
					},
					values: []string{"42"},
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
					values:   []string{"File(a.cue)", "File(b.cue)"},
				},
				"x": {
					docs: map[string]string{
						"x @ a.cue:4": "// x in file a",
						"x @ b.cue:4": "// x in file b",
					},
					values: []string{"1", "2"},
				},
				"y": {
					docs: map[string]string{
						"y @ b.cue:7": "// only in b",
					},
					values: []string{`"b"`},
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
					docs: map[string]string{
						"outer @ a.cue:4": "// outer is a struct.",
					},
				},
				"outer.inner": {
					children: []string{},
					docs: map[string]string{
						"inner @ a.cue:6": "// inner is a number.",
					},
					values: []string{"42"},
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
				},
				"y.a": {
					values: []string{"1"},
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
				"": {
					docs: map[string]string{},
				},
				"x": {
					docs:   map[string]string{},
					values: []string{"1"},
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
	// docs, if non-nil, is the expected map from rendered key node to
	// joined comment text for all entries returned by DocComments() at
	// this node. Key nodes are rendered as "<keyText> @
	// <filename>:<line>" to distinguish identically-named declarations
	// across files.
	docs map[string]string
	// values, if non-nil, is the expected formatted text of every node
	// yielded by Values() at this node, sorted
	// lexicographically. Files are rendered as "File(<filename>)".
	values []string
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
				if want.docs != nil {
					got := renderDocs(node)
					qt.Check(t, qt.DeepEquals(got, want.docs), qt.Commentf("path %q: DocComments()", path))
				}
				if want.values != nil {
					got := renderValues(node)
					qt.Check(t, qt.DeepEquals(got, want.values), qt.Commentf("path %q: Values()", path))
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

func renderDocs(n *eval.Node) map[string]string {
	out := make(map[string]string)
	for key, groups := range n.DocComments() {
		var keyText string
		switch k := key.(type) {
		case *ast.Ident:
			keyText = k.Name
		case *ast.BasicLit:
			keyText = k.Value
		default:
			keyText = fmt.Sprintf("%T", key)
		}
		pos := key.Pos().Position()
		fullKey := fmt.Sprintf("%s @ %s:%d", keyText, pos.Filename, pos.Line)

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
		out[fullKey] = b.String()
	}
	return out
}

func renderValues(n *eval.Node) []string {
	var out []string
	for v := range n.Values() {
		if f, ok := v.(*ast.File); ok {
			out = append(out, fmt.Sprintf("File(%s)", f.Filename))
			continue
		}
		b, err := format.Node(v)
		if err != nil {
			out = append(out, fmt.Sprintf("<err:%v>", err))
			continue
		}
		out = append(out, strings.TrimSpace(string(b)))
	}
	slices.Sort(out)
	return out
}
