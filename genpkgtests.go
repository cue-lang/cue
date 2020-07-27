// +build ignore

package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/ioutil"
	"os"

	"strconv"

	"github.com/rogpeppe/go-internal/txtar"
)

func main() {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "./cue/builtin_test.go", nil, 0)
	if err != nil {
		fmt.Println(err)
		return
	}

	m := map[string]*txtar.Archive{}
	count := map[string]int{}

	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fun.(*ast.Ident)
		if !ok || ident.Name != "test" {
			return true
		}

		str := call.Args[0].(*ast.BasicLit)
		pkg, _ := strconv.Unquote(str.Value)
		a := &txtar.Archive{
			Comment: []byte(
				"# generated from the original tests.\n# Henceforth it may be nicer to group tests into separate files."),
			Files: []txtar.File{{Name: "in.cue"}},
		}
		m[pkg] = a
		return false
	})

	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fun.(*ast.Ident)
		if !ok || ident.Name != "test" {
			return true
		}

		str := call.Args[0].(*ast.BasicLit)
		pkg, _ := strconv.Unquote(str.Value)
		str = call.Args[1].(*ast.BasicLit)
		expr, err := strconv.Unquote(str.Value)
		if err != nil {
			panic(err)
		}

		a := m[pkg]
		count[pkg]++

		a.Files[0].Data = append(a.Files[0].Data,
			fmt.Sprintf("t%d: %s\n", count[pkg], expr)...)

		return false
	})

	for key, a := range m {
		os.Mkdir(fmt.Sprintf("pkg/%s/testdata", key), 0755)
		p := fmt.Sprintf("pkg/%s/testdata/gen.txtar", key)
		ioutil.WriteFile(p, txtar.Format(a), 0644)
	}
}
