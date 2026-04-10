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

package wasm

import (
	"fmt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
)

// argList returns the types of a function's arguments and result
// (specified as an external attribute) as a list of strings.
func argList(a *internal.Attr) ([]string, error) {
	sig, err := sig(a)
	if err != nil {
		return nil, err
	}
	f, err := parseFunc(sig)
	if err != nil {
		return nil, err
	}
	return args(f), nil
}

// sig returns the function signature specified in an external attribute.
func sig(a *internal.Attr) (string, error) {
	sig, ok, err := a.Lookup(1, "sig")
	if err != nil {
		return "", err
	}
	if !ok {
		return "", errors.New(`missing "sig" key`)
	}
	return sig, nil
}

func parseFunc(sig string) (*ast.Func, error) {
	expr, err := parser.ParseExpr("", sig, parser.ParseFuncs)
	if err != nil {
		return nil, err
	}
	f, ok := expr.(*ast.Func)
	if !ok {
		// TODO: once we have position information, make this
		// error more user-friendly by returning the position.
		return nil, errors.New("not a function")
	}
	for _, arg := range append(f.Args, f.Ret) {
		switch arg.(type) {
		case *ast.Ident, *ast.SelectorExpr:
			continue
		default:
			// TODO: once we have position information, make this
			// error more user-friendly by returning the position.
			return nil, errors.Newf(token.NoPos, "expected identifier, found %T", arg)
		}
	}
	return f, nil
}

func args(f *ast.Func) []string {
	var args []string
	for _, arg := range append(f.Args, f.Ret) {
		switch v := arg.(type) {
		case *ast.Ident:
			args = append(args, v.Name)
		case *ast.SelectorExpr:
			b, _ := format.Node(v)
			args = append(args, string(b))
		default:
			panic(fmt.Sprintf("unexpected type: %T", v))
		}
	}
	return args
}

// compileStringsInScope takes a list of strings, compiles them using
// the provided scope, and returns the compiled values.
func compileStringsInScope(strs []string, scope cue.Value) []cue.Value {
	var vals []cue.Value
	for _, typ := range strs {
		val := scope.Context().CompileString(typ, cue.Scope(scope))
		vals = append(vals, val)
	}
	return vals
}

func splitLast[T any](x []T) ([]T, T) {
	return x[:len(x)-1], x[len(x)-1]
}
