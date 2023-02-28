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

package runtime

import (
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/walk"
)

// SetInterpreter sets the interpreter for extern type key.
func (r *Runtime) SetInterpreter(key string, i Interpreter) {
	if r.interpreters == nil {
		r.interpreters = map[string]Interpreter{}
	}
	r.interpreters[key] = i
}

// Interpreter defines an entrypoint for creating per-package interpreters.
type Interpreter interface {
	// Init creates a compiler for built instance b and reports any errors.
	Init(b *build.Instance) (Compiler, errors.Error)
}

// A Compiler composes an adt.Builtin for an external function implementation.
type Compiler interface {
	// Compile creates a builtin for the given function name and attribute.
	// funcName is set to "name" value in a if it exists.
	Compile(funcName string, a *internal.Attr) (*adt.Builtin, errors.Error)
}

func (r *Runtime) injectImplementations(b *build.Instance, v *adt.Vertex) (errs errors.Error) {
	if r.interpreters == nil {
		return nil
	}

	d := &externDecorator{
		runtime: r,
		pkg:     b,
	}

	for _, f := range b.Files {
		d.errs = errors.Append(d.errs, d.addFile(f))
	}

	for _, c := range v.Conjuncts {
		d.decorateConjunct(c.Elem())
	}

	return d.errs
}

// externDecorator locates extern attributes and calls the relevant interpreters
// to inject builtins.
//
// This is a two-pass algorithm: in the first pass, all ast.Files are processed
// to build an index from *ast.Fields to attributes. In the second phase, the
// corresponding adt.Fields are located in the ADT and decorated with the
// builtins.
type externDecorator struct {
	runtime *Runtime
	pkg     *build.Instance

	runtimes   map[string]Compiler
	fields     map[*ast.Field]fieldInfo
	fieldStack []*ast.Field

	errs errors.Error
}

type fieldInfo struct {
	file     *ast.File
	extern   string
	funcName string
	attrBody string
	attr     *ast.Attribute
}

// addFile finds injection points for external implementations of
// Builtins.
func (d *externDecorator) addFile(f *ast.File) (errs errors.Error) {
	var (
		hasPkg   = false
		i        = 0
		kind     string
		fileAttr *ast.Attribute
	)

	// Only process files with file-level "@extern(name)" attribute.
loop:
	for ; i < len(f.Decls); i++ {
		switch a := f.Decls[i].(type) {
		case *ast.Package:
			hasPkg = true
			break loop

		case *ast.Attribute:
			key, body := a.Split()
			if key != "extern" {
				continue
			}
			fileAttr = a

			attr := internal.ParseAttrBody(a.Pos(), body)
			if attr.Err != nil {
				return attr.Err
			}
			k, err := attr.String(0)
			if err != nil {
				// Unreachable.
				return errors.Newf(a.Pos(), "%s", err)
			}

			if k == "" {
				return errors.Newf(a.Pos(), "interpreter name must be non-empty")
			}

			if kind != "" {
				return errors.Newf(a.Pos(),
					"only one file-level extern attribute allowed per file")

			}
			kind = k

			if d.runtimes == nil {
				d.runtimes = map[string]Compiler{}
				d.fields = map[*ast.Field]fieldInfo{}
			}

			if d.runtimes[kind] != nil {
				break
			}

			x := d.runtime.interpreters[kind]
			if x == nil {
				return errors.Newf(a.Pos(), "no interpreter defined for %q", kind)
			}

			fn, cerr := x.Init(d.pkg)
			if cerr != nil {
				return cerr
			}
			if fn == nil {
				return nil
			}
			d.runtimes[kind] = fn
		}
	}

	switch {
	case fileAttr != nil && hasPkg:
		// Success: continue

	case fileAttr == nil && !hasPkg:
		// Nothing to see here.
		return nil

	case fileAttr != nil && !hasPkg:
		return errors.Append(errs, errors.Newf(fileAttr.Pos(),
			"extern attribute without package clause"))

	case fileAttr == nil && hasPkg:
		// Check that there are no top-level extern attributes.
		for i++; i < len(f.Decls); i++ {
			x, ok := f.Decls[i].(*ast.Attribute)
			if !ok {
				continue
			}
			if key, _ := x.Split(); key == "extern" {
				errs = errors.Append(errs, errors.Newf(x.Pos(),
					"extern attribute must appear before package clause"))
			}
		}
		return errs
	}

	// Collect all *ast.Fields with extern attributes.
	// Both of the following forms are allowed:
	//  	a: _ @extern(...)
	//      a: { _, @extern(...) }
	// consistent with attribute implementation recommendations.
	ast.Walk(&ast.File{Decls: f.Decls[i:]}, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.Field:
			d.fieldStack = append(d.fieldStack, x)

		case *ast.Attribute:
			key, body := x.Split()
			if key != "extern" {
				break
			}

			lastField := len(d.fieldStack) - 1
			if lastField < 0 {
				errs = errors.Append(errs, errors.Newf(x.Pos(),
					"extern attribute not associated with field"))
				return true
			}

			f := d.fieldStack[lastField]

			if _, ok := d.fields[f]; ok {
				errs = errors.Append(errs, errors.Newf(x.Pos(),
					"duplicate extern attributes"))
				return true
			}

			name, isIdent, err := ast.LabelName(f.Label)
			if err != nil || !isIdent {
				b, _ := format.Node(f.Label)
				errs = errors.Append(errs, errors.Newf(x.Pos(),
					"can only define functions for fields with identifier names, found %v", string(b)))
			}

			d.fields[f] = fieldInfo{
				extern:   kind,
				funcName: name,
				attrBody: body,
				attr:     x,
			}
		}

		return true

	}, func(n ast.Node) {
		switch n.(type) {
		case *ast.Field:
			d.fieldStack = d.fieldStack[:len(d.fieldStack)-1]
		}
	})

	return errs
}

func (d *externDecorator) decorateConjunct(e adt.Elem) {
	w := walk.Visitor{Before: d.processADTNode}
	w.Elem(e)
}

func (d *externDecorator) processADTNode(n adt.Node) bool {
	f, ok := n.(*adt.Field)
	if !ok {
		return true
	}

	info, ok := d.fields[f.Src]
	if !ok {
		return true
	}

	ic, ok := d.runtimes[info.extern]
	if !ok {
		// An error for a missing runtime was already reported earlier,
		// if applicable.
		return true
	}

	attr := internal.ParseAttrBody(info.attr.Pos(), info.attrBody)
	if attr.Err != nil {
		d.errs = errors.Append(d.errs, attr.Err)
		return true
	}
	name := info.funcName
	if str, ok, _ := attr.Lookup(1, "name"); ok {
		name = str
	}

	b, err := ic.Compile(name, &attr)
	if err != nil {
		d.errs = errors.Append(d.errs, err)
		return true
	}

	f.Value = &adt.BinaryExpr{
		Op: adt.AndOp,
		X:  f.Value,
		Y:  b,
	}

	return true
}
