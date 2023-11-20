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
	"path/filepath"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
	coreruntime "cuelang.org/go/internal/core/runtime"
)

// interpreter is a [cuecontext.ExternInterpreter] for Wasm files.
type interpreter struct{}

// New returns a new Wasm interpreter as a [cuecontext.ExternInterpreter]
// suitable for passing to [cuecontext.New].
func New() cuecontext.ExternInterpreter {
	return &interpreter{}
}

func (i *interpreter) Kind() string {
	return "wasm"
}

// NewCompiler returns a Wasm compiler that services the specified
// build.Instance.
func (i *interpreter) NewCompiler(b *build.Instance, _ *coreruntime.Runtime) (coreruntime.Compiler, errors.Error) {
	return &compiler{
		b:         b,
		instances: make(map[string]*instance),
	}, nil
}

// A compiler is a [coreruntime.Compiler]
// that provides Wasm functionality to the runtime.
type compiler struct {
	b *build.Instance

	// instances maps absolute file names to compiled Wasm modules
	// loaded into memory.
	instances map[string]*instance
}

// Compile searches for a Wasm function described by the given `@extern`
// attribute and returns it as an [adt.Builtin] with the given function
// name.
func (c *compiler) Compile(funcName string, _ adt.Value, a *internal.Attr) (adt.Expr, errors.Error) {
	file, err := fileName(a)
	if err != nil {
		return nil, errors.Promote(err, "invalid attribute")
	}
	// TODO: once we have position information, make this
	// error more user-friendly by returning the position.
	if !strings.HasSuffix(file, "wasm") {
		return nil, errors.Newf(token.NoPos, "load %q: invalid file name", file)
	}

	file, found := findFile(file, c.b)
	if !found {
		return nil, errors.Newf(token.NoPos, "load %q: file not found", file)
	}

	inst, err := c.instance(file)
	if err != nil {
		return nil, errors.Newf(token.NoPos, "can't load Wasm module: %v", err)
	}

	funcType, err := funcType(a)
	if err != nil {
		return nil, errors.Newf(token.NoPos, "invalid function signature: %v", err)
	}

	builtin, err := builtin(funcName, funcType, inst)
	if err != nil {
		return nil, errors.Newf(token.NoPos, "can't instantiate function: %v", err)
	}
	return builtin, nil
}

// instance returns the instance corresponding to filename, compiling
// and loading it if necessary.
func (c *compiler) instance(filename string) (inst *instance, err error) {
	inst, ok := c.instances[filename]
	if !ok {
		inst, err = compileAndLoad(filename)
		if err != nil {
			return nil, err
		}
		c.instances[filename] = inst
	}
	return inst, nil
}

// findFile searches the build.Instance for name. If found, it returnes
// its full path name and true, otherwise it returns the original name
// and false.
func findFile(name string, b *build.Instance) (path string, found bool) {
	for _, f := range b.UnknownFiles {
		if f.Filename == name {
			return filepath.Join(b.Dir, name), true
		}
	}
	return name, false
}

// fileName returns the file name of the external module specified in a,
// which must be an extern attribute.
func fileName(a *internal.Attr) (string, error) {
	file, err := a.String(0)
	if err != nil {
		return "", err
	}
	return file, nil
}

// funcType parses the attribute and returns the found function signature
// as a fnTyp, or an error.
func funcType(a *internal.Attr) (fnTyp, error) {
	funcSig, ok, err := a.Lookup(1, "sig")
	if err != nil {
		return fnTyp{}, err
	}
	if !ok {
		return fnTyp{}, errors.New(`missing "sig" key`)
	}
	return parseFuncSig(funcSig)
}

// parseFuncSig parses the string and returns the found function
// signature as a fnTyp, or an error.
func parseFuncSig(sig string) (fnTyp, error) {
	expr, err := parser.ParseExpr("", sig, parser.ParseFuncs)
	if err != nil {
		return fnTyp{}, err
	}
	return toFnTyp(expr)
}

// toFnTyp convert e, which must be an *ast.Func, to a fnTyp.
func toFnTyp(e ast.Expr) (fnTyp, error) {
	f, ok := e.(*ast.Func)
	if !ok {
		// TODO: once we have position information, make this
		// error more user-friendly by returning the position.
		return fnTyp{}, errors.New("not a function")
	}

	var args []typ
	for _, arg := range append(f.Args, f.Ret) {
		switch v := arg.(type) {
		case *ast.Ident:
			args = append(args, typ(v.Name))
		default:
			// TODO: once we have position information, make this
			// error more user-friendly by returning the position.
			return fnTyp{}, errors.Newf(token.NoPos, "expected identifier, found %T", v)
		}
	}
	return fnTyp{
		args: args[:len(args)-1],
		ret:  args[len(args)-1],
	}, nil
}
