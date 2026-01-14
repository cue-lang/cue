package eval_test

import (
	"os"
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/lsp/eval"
	"github.com/go-quicktest/qt"
	"golang.org/x/tools/txtar"
)

func TestDebug(t *testing.T) {
	archive := `-- a.cue --
c: {x.y, e}
x: {c.d, f}
e: d: a: 1
f: y: b: 2
`
	offset := 6

	ar := txtar.Parse([]byte(archive))

	var files []*ast.File
	filesByName := make(map[string]*ast.File)
	filesByPkg := make(map[string][]*ast.File)

	for _, fh := range ar.Files {
		fileAst, _ := parser.ParseFile(fh.Name, fh.Data, parser.ParseComments)
		fileAst.Pos().File().SetContent(fh.Data)
		qt.Assert(t, qt.IsNotNil(fileAst))
		files = append(files, fileAst)
		filesByName[fh.Name] = fileAst
		pkgName := fileAst.PackageName()
		filesByPkg[pkgName] = append(filesByPkg[pkgName], fileAst)
	}

	evalByFilename := make(map[string]*eval.FileEvaluator)
	evalByPkgName := make(map[string]*eval.Evaluator)
	forPackage := func(importPath ast.ImportPath) *eval.Evaluator {
		return evalByPkgName[importPath.String()]
	}
	importCanonicalisation := make(map[string]ast.ImportPath)

	for pkgName, files := range filesByPkg {
		ip := ast.ImportPath{Path: pkgName}.Canonical()
		importCanonicalisation[pkgName] = ip
		eval := eval.New(ip, importCanonicalisation, forPackage, nil, files...)
		evalByPkgName[pkgName] = eval
		for _, fileAst := range files {
			evalByFilename[fileAst.Filename] = eval.ForFile(fileAst.Filename)
		}
	}

	for _, e := range evalByPkgName {
		vis := eval.NewVisualiser(e)
		fe := e.ForFile("a.cue")
		fe.DefinitionsForOffset(offset)
		os.WriteFile("/tmp/dia.d2", []byte(vis.Render()), 0o666)
	}
}
