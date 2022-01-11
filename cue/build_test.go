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

package cue_test

import (
	"fmt"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/debug"
	"cuelang.org/go/internal/value"
)

func TestFromExpr(t *testing.T) {
	testCases := []struct {
		expr ast.Expr
		out  string
	}{{
		expr: ast.NewString("Hello"),
		out:  `"Hello"`,
	}, {
		expr: ast.NewList(
			ast.NewString("Hello"),
			ast.NewString("World"),
		),
		out: `["Hello", "World"]`,
	}}
	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			r := cuecontext.New()
			v := r.BuildExpr(tc.expr)
			if err := v.Err(); err != nil {
				t.Fatal(err)
			}
			if got := fmt.Sprint(v); got != tc.out {
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
		"example.com/foo/pkg2:pkg",
		files(`
		package pkg

		Number: 12
		`),
	}
	pkg3 := &bimport{
		"example.com/foo/v1:pkg3",
		files(`
		package pkg3

		List: [1,2,3]
		`),
	}

	testCases := []struct {
		instances []*bimport
		emit      string
	}{{
		insts(&bimport{"", files(`test: "ok"`)}),
		`{test:"ok"}`,
	}, {
		insts(&bimport{"",
			files(
				`package test

				import "math"

				"Pi: \(math.Pi)!"`)}),
		`"Pi: 3.14159265358979323846264338327950288419716939937510582097494459!"`,
	}, {
		insts(&bimport{"",
			files(
				`package test

				import math2 "math"

				"Pi: \(math2.Pi)!"`)}),
		`"Pi: 3.14159265358979323846264338327950288419716939937510582097494459!"`,
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

				"Hello \(pkg1.Object)!"`),
		}),
		`"Hello World!"`,
	}, {
		insts(pkg1, &bimport{"",
			files(
				`package test

				import pkg2 "pkg1"
				#pkg1: pkg2.Object

				"Hello \(#pkg1)!"`),
		}),
		`"Hello World!"`,
	}, {
		insts(pkg1, pkg2, &bimport{"",
			files(
				`package test

				import bar "pkg1"
				import baz "example.com/foo/pkg2:pkg"

				pkg1: Object: 3
				"Hello \(pkg1.Object)!"`),
		}),
		`imported and not used: "pkg1" as bar (and 1 more errors)`,
	}, {
		insts(pkg2, &bimport{"",
			files(
				`package test

				import "example.com/foo/pkg2:pkg"

				"Hello \(pkg2.Number)!"`),
		}),
		`imported and not used: "example.com/foo/pkg2:pkg" (and 1 more errors)`,
		// `file0.cue:5:14: unresolved reference pkg2`,
	}, {
		insts(pkg2, &bimport{"",
			files(
				`package test

				import "example.com/foo/pkg2:pkg"

				"Hello \(pkg.Number)!"`),
		}),
		`"Hello 12!"`,
	}, {
		insts(pkg3, &bimport{"",
			files(
				`package test

				import "example.com/foo/v1:pkg3"

				"Hello \(pkg3.List[1])!"`),
		}),
		`"Hello 2!"`,
	}, {
		insts(pkg3, &bimport{"",
			files(
				`package test

				import "example.com/foo/v1:pkg3"

				pkg3: 3

				"Hello \(pkg3.List[1])!"`),
		}),
		`pkg3 redeclared as imported package name
	previous declaration at file0.cue:5:5`,
	}}
	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			insts := cue.Build(makeInstances(tc.instances))
			var got string
			if err := insts[0].Err; err != nil {
				got = err.Error()
			} else {
				cfg := &debug.Config{Compact: true}
				r, v := value.ToInternal(insts[0].Value())
				got = debug.NodeString(r, v, cfg)
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
	bi := b.imports[path]
	if bi == nil {
		return nil
	}
	return b.build(bi)
}

type bimport struct {
	path  string // "" means top-level
	files []string
}

func makeInstances(insts []*bimport) (instances []*build.Instance) {
	b := builder{
		ctxt:    build.NewContext(),
		imports: map[string]*bimport{},
	}
	for _, bi := range insts {
		if bi.path != "" {
			b.imports[bi.path] = bi
		}
	}
	for _, bi := range insts {
		if bi.path == "" {
			instances = append(instances, b.build(bi))
		}
	}
	return
}

func (b *builder) build(bi *bimport) *build.Instance {
	path := bi.path
	if path == "" {
		path = "dir"
	}
	p := b.ctxt.NewInstance(path, b.load)
	for i, f := range bi.files {
		_ = p.AddFile(fmt.Sprintf("file%d.cue", i), f)
	}
	_ = p.Complete()
	return p
}
