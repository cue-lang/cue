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
	"slices"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

// info contains information needed to update files.
type info struct {
	tcType reflect.Type

	needsUpdate bool // an updateable field has changed

	table *ast.CompositeLit // the table that is the source of the tests

	testPkg *packages.Package

	calls   map[token.Position]*callInfo
	patches map[ast.Node]ast.Expr

	// err is why the test could not be analyzed, if it could not.
	// The fields above may then be incomplete.
	err error
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
		tcType:  reflect.TypeFor[T](),
		calls:   make(map[token.Position]*callInfo),
		patches: make(map[ast.Node]ast.Expr),
	}
	s.info = info
	// In cuetdtest the function of the test may be a subtest, like TestFoo/v3.
	// We need to strip the last part.
	testName, _, _ := strings.Cut(s.t.Name(), "/")
	info.err = info.load(testName, file)
	return info
}

// load analyzes the source of the named test function, given the file of
// an Equal call which needs the analysis.
func (i *info) load(testName, file string) error {
	pkgs, err := loadPackages()
	if err != nil {
		return fmt.Errorf("load: %v", err)
	}

	// Find the function declaration of this test, which may be in a different
	// file or package than the Equal call which needs the info.
	f, fn, pkg := findTestFunc(testName, file, pkgs)
	if fn == nil {
		return fmt.Errorf("could not find test %q", testName)
	}
	i.testPkg = pkg

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

	// Find CompositLit table used for the test:
	// - find call to which CompositLit was passed,
	a := i.findCalls(fn.Body, "New", "Run")
	if len(a) != 1 {
		// TODO: allow more than one.
		return fmt.Errorf("only one Run or New function allowed per test")
	}

	// - analyse second argument of call,
	call := a[0].ast
	fset := i.testPkg.Fset
	ti := i.testPkg.TypesInfo
	ident, ok := call.Args[1].(*ast.Ident)
	if !ok {
		return fmt.Errorf("%v: arg 2 of %s must be a reference to the table",
			fset.Position(call.Args[1].Pos()), a[0].funcName)
	}
	def := ti.Uses[ident]
	pos := def.Pos()

	// - locate the CompositeLit in the AST based on position.
	v0 := findVar(pos, f)
	if v0 == nil {
		return fmt.Errorf("cannot find composite literal in source code")
	}
	v, ok := v0.(*ast.CompositeLit)
	if !ok {
		// generics should avoid this.
		return fmt.Errorf("expected composite literal, found %T", v0)
	}
	i.table = v

	// Find and index assertion calls.
	a = i.findCalls(fn.Body, "Equal")
	for _, x := range a {
		if err := i.initFieldRef(x); err != nil {
			return err
		}
	}
	return nil
}

// initFieldRef updates c with information about the field referenced
// in its corresponding call:
//   - name of the field
//   - indexes the field based on filename and line number.
func (i *info) initFieldRef(c *callInfo) error {
	call := c.ast
	info := i.testPkg.TypesInfo
	fset := i.testPkg.Fset
	pos := fset.Position(call.Pos())

	sel, ok := call.Args[1].(*ast.SelectorExpr)
	s := info.Selections[sel]
	if !ok || s == nil || s.Kind() != types.FieldVal {
		return fmt.Errorf("%v: arg 2 of %s must be a reference to a test case field",
			fset.Position(call.Args[1].Pos()), c.funcName)
	}

	obj := s.Obj()
	c.fieldName = obj.Name()
	if _, ok := i.tcType.FieldByName(c.fieldName); !ok {
		return fmt.Errorf("%v: could not find field %s",
			fset.Position(obj.Pos()), c.fieldName)
	}

	pos.Column = 0
	pos.Offset = 0
	i.calls[pos] = c
	return nil
}

// findTestFunc locates the declaration of the named test function within the
// given slice of packages, along with the file and package which hold it.
// A package and its external test package may both declare a test with the
// same name, so the package holding the given file is searched first.
func findTestFunc(name, file string, pkgs []*packages.Package) (*ast.File, *ast.FuncDecl, *packages.Package) {
	for _, sameFile := range []bool{true, false} {
		for _, p := range pkgs {
			if slices.Contains(p.GoFiles, file) != sameFile {
				continue
			}
			for _, f := range p.Syntax {
				for _, d := range f.Decls {
					if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == name {
						return f, fd, p
					}
				}
			}
		}
	}
	return nil, nil, nil
}

func isT(s string) bool {
	// TODO: parametrize this so that tdtest does not have to know of cuetdtest.
	return s == "*cuelang.org/go/internal/tdtest.T" ||
		s == "*cuelang.org/go/internal/cuetdtest.T" ||
		s == "*cuelang.org/go/internal/cuetest.T"
}

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
				if isT(receiver) {
					// Method.
				} else if len(c.Args) == 3 {
					// Run function.
					typ := info.TypeOf(c.Args[2])
					if typ == nil {
						return true
					}
					sig, ok := typ.Underlying().(*types.Signature)
					if !ok || sig.Params().Len() != 2 || !isT(sig.Params().At(0).Type().String()) {
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
	case reflect.Int, reflect.Int64, reflect.Int32, reflect.Int16, reflect.Int8:
		i := x.Int()
		value = &ast.BasicLit{Kind: token.INT,
			Value: strconv.FormatInt(i, 10)}
		isZero = i == 0
	case reflect.Uint, reflect.Uint64, reflect.Uint32, reflect.Uint16, reflect.Uint8, reflect.Uintptr:
		i := x.Uint()
		value = &ast.BasicLit{Kind: token.INT,
			Value: strconv.FormatUint(i, 10)}
		isZero = i == 0
	}

	for _, x := range e.Elts {
		kv, ok := x.(*ast.KeyValueExpr)
		if !ok {
			t.Fatalf("%v: elements must be key value pairs",
				fset.Position(x.Pos()))
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
