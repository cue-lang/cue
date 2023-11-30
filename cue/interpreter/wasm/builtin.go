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
	"cuelang.org/go/cue"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/pkg"
	"cuelang.org/go/internal/value"
)

// generateCallThatReturnsBuiltin returns a call expression to a nullary
// builtin that returns another builtin that corresponds to the external
// Wasm function declared by the user. name is the name of the function,
// args are its declared arguments, scope is a CUE value that represents
// the structure into which to resolve the arguments and i is the
// loaded Wasm instance that contains the function.
//
// This function is implemented as a higher-order function to solve a
// bootstrapping issue. The user can specifies arbitrary types for the
// function's arguments, and these types can be arbitrary CUE types
// defined in arbitrary places. This function is called before CUE
// evaluation at a time where identifiers are not yet resolved. This
// higher-order design solves the bootstrapping issue by deferring the
// resolution of identifiers (and selectors, and expressions in general)
// until runtime. At compile-time we only generate a nullary builtin
// that we call, and being nullary, it does not need to refer to any
// unresolved identifiers, rather the identifiers (and selectors) are
// saved in the closure that executes later, at runtime.
//
// Additionally, resolving identifiers (and selectors) requires an
// OpContext, and we do not have an OpContext at compile time. With
// this higher-order design we get an appropiate OpContext when the
// runtime calls the nullary builtin hence solving the bootstrapping
// problem.
func generateCallThatReturnsBuiltin(name string, scope adt.Value, args []string, i *instance) (adt.Expr, error) {
	// ensure that the function exists before trying to call it.
	_, err := i.load(name)
	if err != nil {
		return nil, err
	}

	call := &adt.CallExpr{Fun: &adt.Builtin{
		Result: adt.TopKind,
		Name:   name,
		Func: func(opctx *adt.OpContext, _ []adt.Value) adt.Expr {
			scope := value.Make(opctx, scope)
			sig := compileStringsInScope(args, scope)
			args, result := splitLast(sig)
			b := &pkg.Builtin{
				Name:   name,
				Params: params(args),
				Result: result.Kind(),
				Func:   cABIFunc(i, name, sig),
			}
			return pkg.ToBuiltin(b)
		},
	}}
	return call, nil
}

// param converts a CUE value that represents the type of a function
// argument into its pkg.Param counterpart.
func param(arg cue.Value) pkg.Param {
	param := pkg.Param{
		Kind: arg.IncompleteKind(),
	}
	if param.Kind == adt.StructKind || ((param.Kind & adt.NumberKind) != 0) {
		_, v := value.ToInternal(arg)
		param.Value = v
	}
	return param
}

// params converts a list of CUE values that represent the types of a
// function's arguments into their pkg.Param counterparts.
func params(args []cue.Value) []pkg.Param {
	var params []pkg.Param
	for _, a := range args {
		params = append(params, param(a))
	}
	return params
}
