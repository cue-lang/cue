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

// qgo builds CUE builtin packages from Go packages.
package main

import (
	"bytes"
	"flag"
	"fmt"
	"go/ast"
	"go/constant"
	"go/format"
	"go/printer"
	"go/token"
	"go/types"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"golang.org/x/tools/go/packages"
)

const help = `
Commands:
extract		Extract one-line signature of exported types of
			the given package.

			Functions that have have more than one return
			argument or unknown types are skipped.
`

// Even though all of the code is generated, the documentation is copied as is.
// So for proper measure, include both the CUE and Go licenses.
const copyright = `// Copyright 2018 The CUE Authors
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

// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
`

var genLine string

var (
	exclude  = flag.String("exclude", "", "comma-separated list of regexps of entries to exclude")
	stripstr = flag.Bool("stripstr", false, "Remove String suffix from functions")
)

func init() {
	log.SetFlags(log.Lshortfile)
}

func main() {
	flag.Parse()

	genLine = "//go:generate " + strings.Join(os.Args, " ")

	args := flag.Args()
	if len(args) == 0 {
		fmt.Println(strings.TrimSpace(help))
		return
	}

	command := args[0]
	args = args[1:]

	switch command {
	case "extract":
		extract(args)
	}
}

var exclusions []*regexp.Regexp

func initExclusions() {
	for _, re := range strings.Split(*exclude, ",") {
		if re != "" {
			exclusions = append(exclusions, regexp.MustCompile(re))
		}
	}
}

func filter(name string) bool {
	if !ast.IsExported(name) {
		return true
	}
	for _, ex := range exclusions {
		if ex.MatchString(name) {
			return true
		}
	}
	return false
}

func pkgName() string {
	pkg, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}
	return filepath.Base(pkg)
}

type extracter struct {
	pkg *packages.Package
}

func extract(args []string) {
	cfg := &packages.Config{
		Mode: packages.LoadFiles |
			packages.LoadAllSyntax |
			packages.LoadTypes,
	}
	pkgs, err := packages.Load(cfg, args...)
	if err != nil {
		log.Fatal(err)
	}

	e := extracter{}

	lastPkg := ""
	var w *bytes.Buffer
	initExclusions()

	flushFile := func() {
		if w != nil && w.Len() > 0 {
			b, err := format.Source(w.Bytes())
			if err != nil {
				log.Fatal(err)
			}
			err = ioutil.WriteFile(lastPkg+".go", b, 0644)
			if err != nil {
				log.Fatal(err)
			}
		}
		w = &bytes.Buffer{}
	}

	for _, p := range pkgs {
		e.pkg = p
		for _, f := range p.Syntax {
			if lastPkg != p.Name {
				flushFile()
				lastPkg = p.Name
				fmt.Fprintln(w, copyright)
				fmt.Fprintln(w, genLine)
				fmt.Fprintln(w)
				fmt.Fprintf(w, "package %s\n", pkgName())
				fmt.Fprintln(w)
				fmt.Fprintf(w, "import %q", p.PkgPath)
				fmt.Fprintln(w)
			}

			for _, d := range f.Decls {
				switch x := d.(type) {
				case *ast.FuncDecl:
					e.reportFun(w, x)
				case *ast.GenDecl:
					e.reportDecl(w, x)
				}
			}
		}
	}
	flushFile()
}

func (e *extracter) reportFun(w io.Writer, x *ast.FuncDecl) {
	if filter(x.Name.Name) {
		return
	}
	pkgName := e.pkg.Name
	override := ""
	params := []ast.Expr{}
	if x.Type.Params != nil {
		for _, f := range x.Type.Params.List {
			tx := f.Type
			if star, isStar := tx.(*ast.StarExpr); isStar {
				if i, ok := star.X.(*ast.Ident); ok && ast.IsExported(i.Name) {
					f.Type = &ast.SelectorExpr{X: ast.NewIdent(pkgName), Sel: i}
					if isStar {
						f.Type = &ast.StarExpr{X: f.Type}
					}
				}
			}
			for _, n := range f.Names {
				params = append(params, n)
				if n.Name == pkgName {
					override = pkgName + x.Name.Name
				}
			}
		}
	}
	var fn ast.Expr = &ast.SelectorExpr{
		X:   ast.NewIdent(pkgName),
		Sel: x.Name,
	}
	if override != "" {
		fn = ast.NewIdent(override)
	}
	x.Body = &ast.BlockStmt{List: []ast.Stmt{
		&ast.ReturnStmt{Results: []ast.Expr{&ast.CallExpr{
			Fun:  fn,
			Args: params,
		}}},
	}}
	if name := x.Name.Name; *stripstr && strings.HasSuffix(name, "String") {
		newName := name[:len(name)-len("String")]
		x.Name = ast.NewIdent(newName)
		if x.Doc != nil {
			for _, c := range x.Doc.List {
				c.Text = strings.Replace(c.Text, name, newName, -1)
			}
		}
	}
	types := []ast.Expr{}
	if x.Recv == nil && x.Type != nil && x.Type.Results != nil && !strings.HasPrefix(x.Name.Name, "New") {
		for _, f := range x.Type.Results.List {
			if len(f.Names) == 0 {
				types = append(types, f.Type)
			} else {
				for range f.Names {
					types = append(types, f.Type)
				}
			}
		}
	}
	if len(types) != 1 {
		switch len(types) {
		case 2:
			if i, ok := types[1].(*ast.Ident); ok && i.Name == "error" {
				break
			}
			fallthrough
		default:
			fmt.Printf("Skipping ")
			x.Doc = nil
			printer.Fprint(os.Stdout, e.pkg.Fset, x)
			fmt.Println()
			return
		}
	}
	fmt.Fprintln(w)
	printer.Fprint(w, e.pkg.Fset, x.Doc)
	printer.Fprint(w, e.pkg.Fset, x)
	fmt.Fprint(w, "\n")
	if override != "" {
		fmt.Fprintf(w, "var %s = %s.%s\n\n", override, pkgName, x.Name.Name)
	}
}

func (e *extracter) reportDecl(w io.Writer, x *ast.GenDecl) {
	if x.Tok != token.CONST {
		return
	}
	k := 0
	for _, s := range x.Specs {
		if v, ok := s.(*ast.ValueSpec); ok && !filter(v.Names[0].Name) {
			if v.Values == nil {
				v.Values = make([]ast.Expr, len(v.Names))
			}
			for i, expr := range v.Names {
				// This check can be removed if we set constants to floats.
				if _, ok := v.Values[i].(*ast.BasicLit); ok {
					continue
				}
				tv, _ := types.Eval(e.pkg.Fset, e.pkg.Types, v.Pos(), v.Names[0].Name)
				tok := token.ILLEGAL
				switch tv.Value.Kind() {
				case constant.Bool:
					v.Values[i] = ast.NewIdent(tv.Value.ExactString())
					continue
				case constant.String:
					tok = token.STRING
				case constant.Int:
					tok = token.INT
				case constant.Float:
					tok = token.FLOAT
				default:
					fmt.Printf("Skipping %s\n", v.Names)
					continue
				}
				v.Values[i] = &ast.BasicLit{
					ValuePos: expr.Pos(),
					Kind:     tok,
					Value:    tv.Value.ExactString(),
				}
			}
			v.Type = nil
			x.Specs[k] = v
			k++
		}
	}
	x.Specs = x.Specs[:k]
	if len(x.Specs) == 0 {
		return
	}
	fmt.Fprintln(w)
	printer.Fprint(w, e.pkg.Fset, x)
	fmt.Fprintln(w)
}
