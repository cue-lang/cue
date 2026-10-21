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
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"strconv"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
)

// info contains information needed to update files.
type info struct {
	tcType reflect.Type

	needsUpdate bool // an updateable field has changed

	fset  *token.FileSet
	file  *ast.File         // the file of the test
	table *ast.CompositeLit // the table that is the source of the tests

	calls   map[token.Position]string // the field compared by each Equal call
	patches map[ast.Node]ast.Expr

	// err is why the test could not be analyzed, if it could not.
	// The fields above may then be incomplete.
	err error
}

func (s *set[T]) getInfo(file string) *info {
	if s.info != nil {
		return s.info
	}
	info := &info{
		tcType:  reflect.TypeFor[T](),
		fset:    token.NewFileSet(),
		calls:   make(map[token.Position]string),
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
// an Equal call which needs the analysis. The analysis is syntactic, so the
// test must be in that same file, and it must call Run with a table variable
// and a function literal which holds the Equal calls.
func (i *info) load(testName, file string) error {
	f, err := parser.ParseFile(i.fset, file, nil, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("load: %v", err)
	}
	i.file = f

	var fn *ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Recv == nil && fd.Name.Name == testName {
			fn = fd
		}
	}
	if fn == nil {
		return fmt.Errorf("could not find test %q", testName)
	}

	// Find CompositLit table used for the test:
	// - find call to which CompositLit was passed,
	a := findCalls(fn.Body, "", "Run", 3)
	if len(a) != 1 {
		// TODO: allow more than one.
		return fmt.Errorf("only one Run function allowed per test")
	}

	// - analyse second argument of call,
	call := a[0]
	ident, ok := call.Args[1].(*ast.Ident)
	if !ok {
		return fmt.Errorf("%v: arg 2 of Run must be a reference to the table",
			i.fset.Position(call.Args[1].Pos()))
	}

	// - locate the CompositeLit in the declaration of the variable.
	v0 := findVar(f, fn, ident.Name, call.Pos())
	if v0 == nil {
		return fmt.Errorf("cannot find composite literal in source code")
	}
	v, ok := v0.(*ast.CompositeLit)
	if !ok {
		// generics should avoid this.
		return fmt.Errorf("expected composite literal, found %T", v0)
	}
	i.table = v

	// Find and index assertion calls, which are on the first parameter
	// of the function literal and refer to a field of the second.
	lit, ok := call.Args[2].(*ast.FuncLit)
	if !ok || len(lit.Type.Params.List) != 2 || len(lit.Type.Params.List[0].Names) != 1 {
		return fmt.Errorf("%v: arg 3 of Run must be a function literal",
			i.fset.Position(call.Args[2].Pos()))
	}
	t := lit.Type.Params.List[0].Names[0].Name
	for _, x := range findCalls(lit.Body, t, "Equal", 2) {
		if err := i.initFieldRef(x); err != nil {
			return err
		}
	}
	return nil
}

// initFieldRef indexes the field referenced by an Equal call
// by the call's filename and line number.
func (i *info) initFieldRef(call *ast.CallExpr) error {
	pos := i.fset.Position(call.Pos())

	sel, ok := call.Args[1].(*ast.SelectorExpr)
	if !ok {
		return fmt.Errorf("%v: arg 2 of Equal must be a reference to a test case field",
			i.fset.Position(call.Args[1].Pos()))
	}

	fieldName := sel.Sel.Name
	if _, ok := i.tcType.FieldByName(fieldName); !ok {
		return fmt.Errorf("%v: could not find field %s",
			i.fset.Position(sel.Sel.Pos()), fieldName)
	}

	pos.Column = 0
	pos.Offset = 0
	i.calls[pos] = fieldName
	return nil
}

// findCalls finds all calls within block to a function or method with the
// given name and at least nargs arguments, called on the identifier recv,
// or on any identifier if recv is empty.
func findCalls(block *ast.BlockStmt, recv, name string, nargs int) []*ast.CallExpr {
	var a []*ast.CallExpr
	ast.Inspect(block, func(n ast.Node) bool {
		c, ok := n.(*ast.CallExpr)
		if !ok || len(c.Args) < nargs {
			return true
		}
		sel, ok := c.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != name {
			return true
		}
		if x, ok := sel.X.(*ast.Ident); ok && (recv == "" || x.Name == recv) {
			a = append(a, c)
		}
		return true
	})
	return a
}

// findVar returns the value of the variable name as seen at pos in fn:
// that of its last declaration in fn before pos, or else of its declaration
// at the top level of f.
func findVar(f *ast.File, fn *ast.FuncDecl, name string, pos token.Pos) (val ast.Expr) {
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		if n == nil || n.Pos() > pos {
			return false
		}
		if v := declValue(n, name); v != nil {
			val = v
		}
		return true
	})
	if val != nil {
		return val
	}
	for _, d := range f.Decls {
		if d, ok := d.(*ast.GenDecl); ok {
			for _, spec := range d.Specs {
				if v := declValue(spec, name); v != nil {
					return v
				}
			}
		}
	}
	return nil
}

// declValue returns the value which n gives to the variable name,
// if n declares it.
func declValue(n ast.Node, name string) ast.Expr {
	switch n := n.(type) {
	case *ast.AssignStmt:
		if n.Tok == token.DEFINE && len(n.Lhs) == len(n.Rhs) {
			for i, x := range n.Lhs {
				if x.(*ast.Ident).Name == name {
					return n.Rhs[i]
				}
			}
		}
	case *ast.ValueSpec:
		if len(n.Names) == len(n.Values) {
			for i, x := range n.Names {
				if x.Name == name {
					return n.Values[i]
				}
			}
		}
	}
	return nil
}

func (s *set[TC]) update() {
	info := s.info

	t := s.t
	fset := info.fset
	f := info.file
	file := fset.File(f.Pos()).Name()

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

func (t *T) updateField(info *info, fieldName string, newValue any) {
	info.needsUpdate = true

	fset := info.fset

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
		if ident.Name == fieldName {
			info.patches[kv.Value] = value
			return
		}
	}

	if !isZero {
		kv := &ast.KeyValueExpr{
			Key:   &ast.Ident{Name: fieldName},
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
