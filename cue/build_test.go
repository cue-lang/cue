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

package cue

import (
	"fmt"
	"strings"
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
)

func TestFromExpr(t *testing.T) {
	testCases := []struct {
		expr ast.Expr
		out  string
	}{{
		expr: &ast.BasicLit{Kind: token.STRING, Value: `"Hello"`},
		out:  `"Hello"`,
	}, {
		expr: &ast.ListLit{Elts: []ast.Expr{
			&ast.BasicLit{Kind: token.STRING, Value: `"Hello"`},
			&ast.BasicLit{Kind: token.STRING, Value: `"World"`},
		}},
		out: `["Hello","World"]`,
	}}
	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			r := &Runtime{}
			inst, err := r.FromExpr(tc.expr)
			if err != nil {
				t.Fatal(err)
			}
			ctx := inst.newContext()
			if got := debugStr(ctx, inst.eval(ctx)); got != tc.out {
				t.Errorf("\n got: %v; want %v", got, tc.out)
			}
		})
	}
}

func TestBuild(t *testing.T) {
	files := func(s ...string) []string { return s }
	insts := func(i ...*bimport) []*bimport { return i }
	pkg1 := &bimport{
		"pkg1",
		files(`
		package pkg1

		Object: "World"
		`),
	}
	pkg2 := &bimport{
		"example.com/foo/pkg2",
		files(`
		package pkg

		Number: 12
		`),
	}
	insts(pkg1, pkg2)

	testCases := []struct {
		instances []*bimport
		emit      string
	}{{
		insts(&bimport{"", files(`test: "ok"`)}),
		`<0>{test: "ok"}`,
		// }, {
		// 	insts(pkg1, &bimport{"",
		// 		files(
		// 			`package test

		// 			import "math"

		// 			"Pi: \(math.Pi)!"`)}),
		// 	`"Pi: 3.14159265358979323846264338327950288419716939937510582097494459!"`,
	}, {
		insts(pkg1, &bimport{"",
			files(
				`package test

				import "pkg1"

				"Hello \(pkg1.Object)!"`),
		}),
		`"Hello World!"`,
	}, {
		insts(pkg1, &bimport{"",
			files(
				`package test

				import "pkg1"
				pkg1: 1

				"Hello \(pkg1.Object)!"`),
		}),
		`pkg1 redeclared as imported package name
	previous declaration at file0.cue:4:5`,
	}, {
		insts(pkg1, &bimport{"",
			files(
				`package test

				import bar "pkg1"

				pkg1 Object: 3
				"Hello \(pkg1.Object)!"`),
		}),
		`imported and not used: "pkg1" as bar`,
	}, {
		insts(pkg2, &bimport{"",
			files(
				`package test

				import "example.com/foo/pkg2"

				"Hello \(pkg2.Number)!"`),
		}),
		`imported and not used: "example.com/foo/pkg2"`,
		// `file0.cue:5:14: unresolved reference pkg2`,
	}, {
		insts(pkg2, &bimport{"",
			files(
				`package test

				import "example.com/foo/pkg2"

				"Hello \(pkg.Number)!"`),
		}),
		`"Hello 12!"`,
	}}
	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			insts := Build(makeInstances(tc.instances))
			var got string
			if err := insts[0].Err; err != nil {
				got = err.Error()
			} else {
				got = strings.TrimSpace(fmt.Sprintf("%s\n", insts[0].Value()))
			}
			if got != tc.emit {
				t.Errorf("\n got: %s\nwant: %s", got, tc.emit)
			}
		})
	}
}

type builder struct {
	ctxt    *build.Context
	imports map[string]*bimport
}

func (b *builder) load(pos token.Pos, path string) *build.Instance {
	p := b.ctxt.NewInstance(path, b.load)
	bi := b.imports[path]
	if bi == nil {
		return nil
	}
	buildInstance(b.imports[path], p)
	return p
}

type bimport struct {
	path  string // "" means top-level
	files []string
}

func makeInstances(insts []*bimport) (instances []*build.Instance) {
	b := builder{
		ctxt:    build.NewContext(build.ParseOptions(parser.ParseComments)),
		imports: map[string]*bimport{},
	}
	for _, bi := range insts {
		if bi.path != "" {
			b.imports[bi.path] = bi
		}
	}
	for _, bi := range insts {
		if bi.path == "" {
			p := b.ctxt.NewInstance("dir", b.load)
			buildInstance(bi, p)
			instances = append(instances, p)
		}
	}
	return
}

func buildInstance(bi *bimport, p *build.Instance) {
	for i, f := range bi.files {
		_ = p.AddFile(fmt.Sprintf("file%d.cue", i), f)
	}
	p.Complete()
}
