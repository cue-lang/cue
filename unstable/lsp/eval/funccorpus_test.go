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
	"path/filepath"
	"testing"

	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/pkg"
	"cuelang.org/go/unstable/lsp/eval"
)

// TestFunctionCorpora queries the evaluator at every offset of every
// standard library definition file, which declares its builtins as
// function signatures, and of every CUE file in the evaluator's
// functions test corpus. It checks only that each query returns: the
// corpora hold function literals of every shape, including erroneous
// ones.
func TestFunctionCorpora(t *testing.T) {
	for _, importPath := range pkg.ImportPaths() {
		src, found := pkg.Source(importPath)
		qt.Assert(t, qt.IsTrue(found))
		t.Run("pkg/"+importPath, func(t *testing.T) {
			queryEveryOffset(t, "cue-stdlib/"+importPath+".cue", src)
		})
	}

	archives, err := filepath.Glob(filepath.Join("..", "..", "..", "cue", "testdata", "functions", "*.txtar"))
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.Not(qt.HasLen(archives, 0)))
	for _, archive := range archives {
		ar, err := txtar.ParseFile(archive)
		qt.Assert(t, qt.IsNil(err))
		for _, file := range ar.Files {
			if filepath.Ext(file.Name) != ".cue" {
				continue
			}
			t.Run(filepath.Base(archive)+"/"+file.Name, func(t *testing.T) {
				queryEveryOffset(t, file.Name, file.Data)
			})
		}
	}
}

// queryEveryOffset parses src, keeping whatever syntax tree the parser
// recovers from errors, and queries the evaluator at every offset.
func queryEveryOffset(t *testing.T, filename string, src []byte) {
	f, _ := parser.ParseFile(filename, src, parser.ParseComments)
	qt.Assert(t, qt.IsNotNil(f))
	f.Pos().File().SetContent(src)
	ev := eval.New(eval.Config{
		IP: ast.ImportPath{Path: f.PackageName()}.Canonical(),
	}, f)
	fe := ev.ForFile(filename)
	qt.Assert(t, qt.IsNotNil(fe))
	for offset := range len(src) + 1 {
		fe.DefinitionNodesForOffset(offset)
		fe.CompletionsForOffset(offset)
		fe.UsagesForOffset(offset, true)
		fe.DeclsForOffset(offset)
	}
}
