// Copyright 2023 CUE Authors
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

package tdtest

import (
	"fmt"
	"go/ast"
	"go/format"
	"go/token"
	"go/types"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

// info contains information needed to update files.
type info struct {
	t *testing.T

	tcType reflect.Type

	needsUpdate bool // an updateable field has changed

	table *ast.CompositeLit // the table that is the source of the tests

	testPkg *packages.Package

	calls   map[token.Position]*callInfo
	patches map[ast.Node]ast.Expr
}

type callInfo struct {
	ast       *ast.CallExpr
	funcName  string
	fieldName string
}

var loadPackages = sync.OnceValues(func() ([]*packages.Package, error) {
	cfg := &packages.Config{
		Mode: packages.NeedFiles |
			packages.NeedDeps |
			packages.NeedTypes |
			packages.NeedTypesInfo |
			packages.NeedSyntax,
		Tests: true,
	}

	return packages.Load(cfg, ".")
})

func (s *set[T]) getInfo(file string) *info {
	if s.info != nil {
		return s.info
	}
	info := &info{
		t:       s.t,
		tcType:  reflect.TypeFor[T](),
		calls:   make(map[token.Position]*callInfo),
		patches: make(map[ast.Node]ast.Expr),
	}
	s.info = info

	t := s.t

	pkgs, pkgsErr := loadPackages()
	if pkgsErr != nil {
		t.Fatalf("load: %v\n", pkgsErr)
	}

	// Get package under test.
	f, pkg := findFileAndPackage(file, pkgs)
	if f == nil {
		t.Fatalf("failed to load package for file %s", file)
	}
	info.testPkg = pkg

	// TODO: not necessary at the moment, but this is tricky so leaving this in
	// so as to not to forget how to do it.
	//
	// for _, p := range pkg.Types.Imports() {
	// 	if p.Path() == "cuelang.org/go/internal/tdtest" {
	// 		info.thisPkg = p
	// 	}
	// }
	// if info.thisPkg == nil {
	// 	t.Fatalf("could not find test package")
	// }

	// Find function declaration of this test.
	var fn *ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == t.Name() {
			fn = fd
		}
	}
	if fn == nil {
		t.Fatalf("could not find test %q in file %q", t.Name(), file)
	}

	// Find CompositLit table used for the test:
	// - find call to which CompositLit was passed,
	a := info.findCalls(fn.Body, "New", "Run")
	if len(a) != 1 {
		// TODO: allow more than one.
		t.Fatalf("only one Run or New function allowed per test")
	}

	// - analyse second argument of call,
	call := a[0].ast
	fset := info.testPkg.Fset
	ti := info.testPkg.TypesInfo
	ident, ok := call.Args[1].(*ast.Ident)
	if !ok {
		t.Fatalf("%v: arg 2 of %s must be a reference to the table",
			fset.Position(call.Args[1].Pos()), a[0].funcName)
	}
	def := ti.Uses[ident]
	pos := def.Pos()

	// - locate the CompositeLit in the AST based on position.
	v0 := findVar(pos, f)
	if v0 == nil {
		t.Fatalf("cannot find composite literal in source code")
	}
	v, ok := v0.(*ast.CompositeLit)
	if !ok {
		// generics should avoid this.
		t.Fatalf("expected composite literal, found %T", v0)
	}
	info.table = v

	// Find and index assertion calls.
	a = info.findCalls(fn.Body, "Equal")
	for _, x := range a {
		info.initFieldRef(x, f)
	}

	return info
}

// initFieldRef updates c with information about the field referenced
// in its corresponding call:
//   - name of the field
//   - indexes the field based on filename and line number.
func (i *info) initFieldRef(c *callInfo, f *ast.File) {
	call := c.ast
	t := i.t
	info := i.testPkg.TypesInfo
	fset := i.testPkg.Fset
	pos := fset.Position(call.Pos())

	sel, ok := call.Args[1].(*ast.SelectorExpr)
	s := info.Selections[sel]
	if !ok || s == nil || s.Kind() != types.FieldVal {
		t.Fatalf("%v: arg 2 of %s must be a reference to a test case field",
			fset.Position(call.Args[1].Pos()), c.funcName)
	}

	obj := s.Obj()
	c.fieldName = obj.Name()
	if _, ok := i.tcType.FieldByName(c.fieldName); !ok {
		t.Fatalf("%v: could not find field %s",
			fset.Position(obj.Pos()), c.fieldName)
	}

	pos.Column = 0
	pos.Offset = 0
	i.calls[pos] = c
}

// findFileAndPackage locates the ast.File and package within the given slice
// of packages, in which the given file is located.
func findFileAndPackage(path string, pkgs []*packages.Package) (*ast.File, *packages.Package) {
	for _, p := range pkgs {
		for i, gf := range p.GoFiles {
			if gf == path {
				return p.Syntax[i], p
			}
		}
	}
	return nil, nil
}

const typeT = "*cuelang.org/go/internal/tdtest.T"

// findCalls finds all call expressions within a given block for functions
// or methods defined within the tdtest package.
func (i *info) findCalls(block *ast.BlockStmt, names ...string) []*callInfo {
	var a []*callInfo
	ast.Inspect(block, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := c.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}

		// TODO: also test package. It would be better to test the equality
		// using the information in the types.Info/packages to ensure that
		// we really got the right function.
		info := i.testPkg.TypesInfo
		for _, name := range names {
			if sel.Sel.Name == name {
				receiver := info.TypeOf(sel.X).String()
				if receiver == typeT {
					// Method.
				} else if len(c.Args) == 3 {
					// Run function.
					fn := c.Args[2].(*ast.FuncLit)
					if len(fn.Type.Params.List) != 2 {
						return true
					}
					argType := info.TypeOf(fn.Type.Params.List[0].Type).String()
					if argType != typeT {
						return true
					}
				} else {
					return true
				}
				ci := &callInfo{
					funcName: name,
					ast:      c,
				}
				a = append(a, ci)
				return true
			}
		}

		return true
	})
	return a
}

func findVar(pos token.Pos, n0 ast.Node) (ret ast.Expr) {
	ast.Inspect(n0, func(n ast.Node) bool {
		if n == nil {
			return true
		}
		switch n := n.(type) {
		case *ast.AssignStmt:
			for i, v := range n.Lhs {
				if v.Pos() == pos {
					ret = n.Rhs[i]
				}
			}
			return false
		case *ast.ValueSpec:
			for i, v := range n.Names {
				if v.Pos() == pos {
					ret = n.Values[i]
				}
			}
			return false
		}
		return true
	})
	return ret
}

func (s *set[TC]) update() {
	info := s.info

	t := s.t
	fset := info.testPkg.Fset

	file := fset.Position(info.table.Pos()).Filename
	var f *ast.File
	for i, gof := range info.testPkg.GoFiles {
		if gof == file {
			f = info.testPkg.Syntax[i]
		}
	}
	if f == nil {
		t.Fatalf("file %s not in package", file)
	}

	// TODO: use text-based insertion instead:
	// - sort insertions and replacements on position in descending order.
	// - substitute textually.
	//
	// We are using Apply because this is supposed to give better handling of
	// comments. In practice this only works marginally better than not handling
	// positions at all. Probably a lost cause.
	astutil.Apply(f, func(c *astutil.Cursor) bool {
		n := c.Node()

		switch x := info.patches[n]; x.(type) {
		case nil:
		case *ast.KeyValueExpr:
			for {
				c.InsertAfter(x)
				x = info.patches[x]
				if x == nil {
					break
				}
			}
		default:
			c.Replace(x)
		}
		return true
	}, nil)

	// TODO: use tmp files?
	w, err := os.Create(file)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()

	err = format.Node(w, fset, f)
	if err != nil {
		t.Fatal(err)
	}
}

func (t *T) updateField(info *info, ci *callInfo, newValue any) {
	info.needsUpdate = true

	fset := info.testPkg.Fset

	e, ok := info.table.Elts[t.iter].(*ast.CompositeLit)
	if !ok {
		t.Fatalf("not a composite literal")
	}

	isZero := false
	var value ast.Expr
	switch x := reflect.ValueOf(newValue); x.Kind() {
	default:
		s := fmt.Sprint(x)
		x = reflect.ValueOf(s)
		fallthrough
	case reflect.String:
		s := x.String()
		isZero = s == ""
		if !strings.ContainsRune(s, '`') && !isZero {
			s = fmt.Sprintf("`%s`", s)
		} else {
			s = strconv.Quote(s)
		}
		value = &ast.BasicLit{Kind: token.STRING, Value: s}
	case reflect.Bool:
		if b := x.Bool(); b {
			value = &ast.BasicLit{Kind: token.IDENT, Value: "true"}
		} else {
			value = &ast.BasicLit{Kind: token.IDENT, Value: "false"}
			isZero = true
		}
	case reflect.Int, reflect.Int64, reflect.Int32, reflect.Int8:
		i := x.Int()
		value = &ast.BasicLit{Kind: token.INT,
			Value: strconv.FormatInt(i, 10)}
		isZero = i == 0
	case reflect.Uint, reflect.Uint64, reflect.Uint32, reflect.Uint8:
		i := x.Uint()
		value = &ast.BasicLit{Kind: token.INT,
			Value: strconv.FormatUint(i, 10)}
		isZero = i == 0
	}

	for _, x := range e.Elts {
		kv, ok := x.(*ast.KeyValueExpr)
		if !ok {
			t.Fatalf("%v: elements must be key value pairs",
				fset.Position(kv.Pos()))
		}
		ident, ok := kv.Key.(*ast.Ident)
		if !ok {
			t.Fatalf("%v: key must be an identifier",
				fset.Position(kv.Pos()))
		}
		if ident.Name == ci.fieldName {
			info.patches[kv.Value] = value
			return
		}
	}

	if !isZero {
		kv := &ast.KeyValueExpr{
			Key:   &ast.Ident{Name: ci.fieldName},
			Value: value,
		}
		if len(e.Elts) > 0 {
			var key ast.Node = e.Elts[len(e.Elts)-1]
			old := info.patches[key]
			if old != nil {
				info.patches[kv] = old
			}
			info.patches[key] = kv
		} else {
			info.patches[e] = &ast.CompositeLit{Elts: []ast.Expr{kv}}
		}
	}
}
