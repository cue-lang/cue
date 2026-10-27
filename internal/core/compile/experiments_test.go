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

package compile_test

import (
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/cueexperiment"
	"github.com/go-quicktest/qt"

	_ "cuelang.org/go/pkg"
)

// TestBuiltFileExperiments checks that the experiments a file carries are the
// ones the compiler goes by. A file a generator assembled has no source, so
// the positions of its declarations say nothing about it; only the file does.
func TestBuiltFileExperiments(t *testing.T) {
	// A let clause naming the predeclared "self" is what the exporter emits
	// for a value alias, and it needs aliasv2 to compile.
	build := func() *ast.File {
		let := &ast.LetClause{
			Ident: ast.NewIdent("X"),
			Expr:  ast.NewPredeclared("self"),
		}
		f := &ast.File{Decls: []ast.Decl{&ast.Field{
			Label: ast.NewIdent("a"),
			Value: &ast.StructLit{Elts: []ast.Decl{
				let,
				&ast.Field{Label: ast.NewIdent("b"), Value: ast.NewIdent("X")},
			}},
		}}}
		astutil.Resolve(f, func(token.Pos, string, ...any) {})
		return f
	}
	compileFile := func(f *ast.File) error {
		_, err := compile.Files(nil, runtime.New(), "", f)
		return err
	}

	// Saying nothing leaves the compiler to assume the defaults of the
	// current language version, where aliasv2 is stable.
	qt.Assert(t, qt.IsNil(compileFile(build())))

	// Experiments the file carries win over that assumption: at a version
	// where aliasv2 is not yet stable, "self" is rejected.
	old := build()
	oldExp, err := cueexperiment.NewFile("v0.17.0")
	qt.Assert(t, qt.IsNil(err))
	old.SetExperiments(oldExp)
	qt.Assert(t, qt.ErrorMatches(compileFile(old),
		`.*predeclared identifier "self" requires @experiment\(aliasv2\).*`))

	// Carrying the current language version's experiments is enough.
	f := build()
	exp, err := cueexperiment.NewFile("")
	qt.Assert(t, qt.IsNil(err))
	f.SetExperiments(exp)
	qt.Assert(t, qt.IsNil(compileFile(f)))

	// The same goes for the postfix ... operator, which the parser rejects
	// before the compiler sees it in source that does not enable explicitopen.
	buildOpen := func(exp *cueexperiment.File) *ast.File {
		f := &ast.File{Decls: []ast.Decl{
			&ast.Field{Label: ast.NewIdent("#A"), Value: ast.NewStruct()},
			&ast.Field{
				Label: ast.NewIdent("a"),
				Value: &ast.PostfixExpr{X: ast.NewIdent("#A"), Op: token.ELLIPSIS},
			},
		}}
		astutil.Resolve(f, func(token.Pos, string, ...any) {})
		f.SetExperiments(exp)
		return f
	}
	qt.Assert(t, qt.ErrorMatches(compileFile(buildOpen(oldExp)),
		`.*postfix \.\.\. operator requires @experiment\(explicitopen\).*`))
	openExp, err := cueexperiment.NewFile("v0.17.0", "explicitopen")
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsNil(compileFile(buildOpen(openExp))))
}
