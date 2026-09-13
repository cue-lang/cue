// Copyright 2021 CUE Authors
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

package astutil_test

import (
	"fmt"
	"path/filepath"
	"testing"
	"text/tabwriter"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/astinternal"
	"cuelang.org/go/internal/cuetxtar"
)

func TestResolve(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root: "./testdata/resolve",
		Name: "resolve",
	}

	test.Run(t, func(t *cuetxtar.Test) {
		// Use RawInstances because we want to allow imports
		// without actually providing any dependencies. In general,
		// Resolve is used just after parsing anyway, so lower level
		// is more appropriate.
		a := t.RawInstances()[0]

		if a.Err != nil {
			b := t.Writer("errors")
			errors.Print(b, a.Err, &errors.Config{Cwd: t.Dir, ToSlash: true})
		}

		for _, f := range a.Files {
			if filepath.Ext(f.Filename) != ".cue" {
				continue
			}

			identMap := map[ast.Node]int{}
			ast.Walk(f, func(n ast.Node) bool {
				switch n.(type) {
				case *ast.File, *ast.StructLit, *ast.Field, *ast.ImportSpec,
					*ast.Ident, *ast.ForClause, *ast.LetClause, *ast.Alias:
					identMap[n] = len(identMap) + 1
				}
				return true
			}, nil)

			// Resolve was already called.

			base := filepath.Base(f.Filename)
			b := t.Writer(base[:len(base)-len(".cue")])
			w := tabwriter.NewWriter(b, 0, 4, 1, ' ', 0)
			ast.Walk(f, func(n ast.Node) bool {
				if x, ok := n.(*ast.Ident); ok {
					fmt.Fprintf(w, "%d[%s]:\tScope: %d[%T]\tNode: %d[%s]\n",
						identMap[x], astinternal.DebugStr(x),
						identMap[x.Scope], x.Scope,
						identMap[x.Node], astinternal.DebugStr(x.Node))
				}
				return true
			}, nil)
			w.Flush()

			fmt.Fprint(b)
		}
	})
}

// TestResolveFuncSignatureScopes tests that a reference to one of a
// function's own parameters in its parameter constraints or return type is
// reserved and reported as an error, and that this rule applies one signature
// at a time: a function literal nested within another function's signature
// resolves its own parameters in its own body as usual.
func TestResolveFuncSignatureScopes(t *testing.T) {
	testCases := []struct {
		desc    string
		src     string
		wantErr []string
	}{{
		desc: "nested function in return type resolves own parameter in body",
		src: `
@experiment(functions)
f: func(x: int) -> (func(a: int) -> int: a): 1
`,
	}, {
		desc: "nested function in parameter constraint resolves own parameter in body",
		src: `
@experiment(functions)
f: func(g: int | *(func(a: int) -> int: a + 1)) -> int: 1
`,
	}, {
		desc: "nested function in body may refer to outer parameter in its signature",
		src: `
@experiment(functions)
f: func(x: int) -> int: {
	g:   func(y: x) -> int: y
	out: g(x)
}.out
`,
	}, {
		desc: "parameter constraint referring to sibling parameter is reserved",
		src: `
@experiment(functions)
f: func(x: int, y: x) -> int: x
`,
		wantErr: []string{
			`cannot refer to parameter "x" in a parameter constraint or return type`,
		},
	}, {
		desc: "return type referring to parameter is reserved",
		src: `
@experiment(functions)
f: func(x: int) -> x: x
`,
		wantErr: []string{
			`cannot refer to parameter "x" in a parameter constraint or return type`,
		},
	}, {
		desc: "nested signature reserves its own parameters",
		src: `
@experiment(functions)
f: func(x: int) -> (func(a: int, b: a) -> int): 1
`,
		wantErr: []string{
			`cannot refer to parameter "a" in a parameter constraint or return type`,
		},
	}, {
		desc: "nested function in outer signature cannot refer to outer parameter",
		src: `
@experiment(functions)
f: func(x: int) -> (func(a: int) -> int: x): 1
`,
		wantErr: []string{
			`cannot refer to parameter "x" in a parameter constraint or return type`,
		},
	}, {
		desc: "parameter default resolves in the enclosing scope",
		src: `
@experiment(functions)
base: 1
f: func(x: int = base) -> int: x
`,
	}, {
		desc: "parameter default referring to sibling parameter is reserved",
		src: `
@experiment(functions)
f: func(x: int, y: int = x) -> int: y
`,
		wantErr: []string{
			`cannot refer to parameter "x" in a parameter constraint or return type`,
		},
	}, {
		desc: "nested function in parameter default resolves own parameter in body",
		src: `
@experiment(functions)
f: func(g: _ = (func(a: int) -> int: a + 1)) -> int: 1
`,
	}}
	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			f, err := parser.ParseFile("test.cue", tc.src)
			if len(tc.wantErr) == 0 {
				qt.Assert(t, qt.IsNil(err))
			}

			// Resolve does not overwrite identifiers that were already
			// resolved during parsing, but the reserved-parameter check runs
			// again.
			var got []string
			astutil.Resolve(f, func(pos token.Pos, msg string, args ...interface{}) {
				got = append(got, fmt.Sprintf(msg, args...))
			})
			qt.Assert(t, qt.DeepEquals(got, tc.wantErr))
		})
	}
}

func TestResolveLegacyFuncArgs(t *testing.T) {
	outerValue := ast.NewIdent("int")
	argRef := ast.NewIdent("outer")
	file := &ast.File{
		Decls: []ast.Decl{
			&ast.Field{
				Label:    ast.NewIdent("outer"),
				TokenPos: token.NoSpace.Pos(),
				Value:    outerValue,
			},
			&ast.Field{
				Label:    ast.NewIdent("f"),
				TokenPos: token.NoSpace.Pos(),
				Value: &ast.Func{
					Func: token.NoSpace.Pos(),
					Args: []ast.Expr{argRef},
					Ret:  ast.NewIdent("_"),
				},
			},
		},
	}

	astutil.Resolve(file, func(pos token.Pos, msg string, args ...interface{}) {
		t.Fatalf(msg, args...)
	})
	if argRef.Node != outerValue {
		t.Fatalf("legacy function argument was not resolved to outer field: got %T", argRef.Node)
	}
}
