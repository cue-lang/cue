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
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/walk"
)

// SetInterpreter sets the interpreter for interpretation of files marked with
// @extern(kind).
func (r *Runtime) SetInterpreter(i Interpreter) {
	if r.interpreters == nil {
		r.interpreters = map[string]Interpreter{}
	}
	r.interpreters[i.Kind()] = i
}

// TODO: consider also passing the top-level attribute to NewCompiler to allow
// passing default values.

// Interpreter defines an entrypoint for creating per-package interpreters.
type Interpreter interface {
	// NewCompiler creates a compiler for b and reports any errors.
	NewCompiler(b *build.Instance, r *Runtime) (Compiler, errors.Error)

	// Kind returns the string to be used in the file-level @extern attribute.
	Kind() string
}

// A Compiler fills in an adt.Expr for fields marked with `@extern(kind)`.
type Compiler interface {
	// Compile creates an adt.Expr (usually a builtin) for the
	// given external named resource (usually a function). name
	// is the name of the resource to compile, taken from altName
	// in `@extern(name=altName)`, or from the field name if that's
	// not defined. Scope is the struct that contains the field.
	// Other than "name", the fields in a are implementation
	// specific.
	Compile(name string, scope adt.Value, a *internal.Attr) (adt.Expr, errors.Error)
}

// InjectImplementations modifies v to include implementations of functions
// for fields associated with the @extern attributes.
//
// TODO(mvdan): unexport again once cue.Instance.Build is no longer used by `cue cmd`
// and can be removed entirely.
func (r *Runtime) InjectImplementations(b *build.Instance, v *adt.Vertex) (errs errors.Error) {

	d := &externDecorator{
		runtime: r,
		pkg:     b,
	}

	for _, f := range b.Files {
		d.errs = errors.Append(d.errs, d.addFile(f))
	}

	for c := range v.LeafConjuncts() {
		d.decorateConjunct(c.Elem(), v)
	}

	return d.errs
}

// externDecorator locates extern attributes and calls the relevant interpreters
// to inject builtins.
type externDecorator struct {
	runtime *Runtime
	pkg     *build.Instance

	compilers map[string]Compiler

	// fileKinds maps each AST file to the set of extern kinds declared in it.
	fileKinds map[*token.File]map[string]bool

	errs errors.Error
}

// addFile finds injection points in the given ast.File for external
// implementations of Builtins.
func (d *externDecorator) addFile(f *ast.File) (errs errors.Error) {
	kinds, _, err := findExternFileAttrs(f)
	if err != nil {
		return err
	}
	if len(kinds) == 0 {
		return nil
	}

	if d.fileKinds == nil {
		d.fileKinds = map[*token.File]map[string]bool{}
	}
	km := make(map[string]bool)
	for kind := range kinds {
		km[kind] = true
	}
	d.fileKinds[f.Pos().File()] = km

	for kind, pos := range kinds {
		if err := d.initCompiler(kind, pos); err != nil {
			errs = errors.Append(errs, err)
		}
	}
	return errs
}

// findExternFileAttrs reports all extern kinds from file-level @extern(kind)
// attributes in f, the position of each corresponding attribute, and f's
// declarations from the package directive onwards. It's an error if duplicate
// @extern attributes for the same kind are found. decls == nil signals that
// this file should be skipped.
func findExternFileAttrs(f *ast.File) (kinds map[string]token.Pos, decls []ast.Decl, err errors.Error) {
	var (
		hasPkg    bool
		p         int
		fileAttrs []token.Pos
	)

loop:
	for ; p < len(f.Decls); p++ {
		switch a := f.Decls[p].(type) {
		case *ast.Package:
			hasPkg = true
			break loop

		case *ast.Attribute:
			if a.Name() != "extern" {
				continue
			}
			attr := internal.ParseAttr(a)
			fileAttrs = append(fileAttrs, attr.Pos)
			if attr.Err != nil {
				err = errors.Append(err, attr.Err)
				continue
			}

			k, e := attr.String(0)
			if e != nil {
				// Unreachable.
				err = errors.Append(err, errors.Newf(attr.Pos, "%s", e))
				continue
			}

			if k == "" {
				err = errors.Append(err, errors.Newf(attr.Pos,
					"interpreter name must be non-empty"))
				continue
			}

			if kinds == nil {
				kinds = map[string]token.Pos{}
			}
			if _, ok := kinds[k]; ok {
				err = errors.Append(err, errors.Newf(attr.Pos,
					"duplicate @extern attribute for kind %q", k))
				continue
			}
			kinds[k] = attr.Pos
		}
	}

	switch {
	case len(fileAttrs) == 0 && !hasPkg:
		return nil, nil, err

	case len(fileAttrs) > 0 && !hasPkg:
		for _, a := range fileAttrs {
			err = errors.Append(err, errors.Newf(a,
				"extern attribute without package clause"))
		}
		return nil, nil, err

	case len(fileAttrs) == 0 && hasPkg:
		// Check that there are no top-level extern attributes.
		for p++; p < len(f.Decls); p++ {
			x, ok := f.Decls[p].(*ast.Attribute)
			if !ok {
				continue
			}
			if key, _ := x.Split(); key == "extern" {
				err = errors.Append(err, errors.Newf(x.Pos(),
					"extern attribute must appear before package clause"))
			}
		}
		return nil, nil, err
	}

	return kinds, f.Decls[p:], err
}

// initCompiler initializes the runtime for kind, if applicable. The pos
// argument represents the position of the file-level @extern attribute.
func (d *externDecorator) initCompiler(kind string, pos token.Pos) errors.Error {
	if _, ok := d.compilers[kind]; ok {
		return nil
	}
	// initialize the compiler.
	if d.compilers == nil {
		d.compilers = map[string]Compiler{}
	}
	x := d.runtime.interpreters[kind]
	if x == nil {
		return errors.Newf(pos, "no interpreter defined for %q", kind)
	}
	c, err := x.NewCompiler(d.pkg, d.runtime)
	if err != nil {
		return err
	}
	d.compilers[kind] = c
	return nil
}

// ExtractAttrsByKind finds all the attributes of the given kind in
// the given AST, parsing their bodies into [internal.Attr].
func ExtractAttrsByKind(file *ast.File, kind string) (attrsByNode map[ast.Node][]*internal.Attr, errs errors.Error) {
	kinds, decls, err := findExternFileAttrs(file)
	if err != nil || len(decls) == 0 {
		return nil, err
	}
	if _, ok := kinds[kind]; !ok {
		return nil, nil
	}

	nodeStack := []ast.Node{file}

	ast.Walk(&ast.File{Decls: decls}, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.StructLit:
			nodeStack = append(nodeStack, n)

		case *ast.Field:
			nodeStack = append(nodeStack, n.Value)

		case *ast.Attribute:
			if n.Name() != kind {
				break
			}

			attrParsed := internal.ParseAttr(n)
			parent := nodeStack[len(nodeStack)-1]
			if attrsByNode == nil {
				attrsByNode = make(map[ast.Node][]*internal.Attr)
			}
			attrsByNode[parent] = append(attrsByNode[parent], attrParsed)
			return false
		}

		return true

	}, func(n ast.Node) {
		switch n.(type) {
		case *ast.StructLit, *ast.Field:
			nodeStack = nodeStack[:len(nodeStack)-1]
		}
	})

	return attrsByNode, errs
}

func (d *externDecorator) decorateConjunct(e adt.Elem, scope *adt.Vertex) {
	w := walk.Visitor{
		Before: func(n adt.Node) bool {
			if s, ok := n.(*adt.StructLit); ok {
				// Only walk the tree for struct literals that
				// are from a file with some extern declarations.
				return s.Src != nil && len(d.fileKinds[s.Src.Pos().File()]) > 0
			}
			return true
		},
		After: func(n adt.Node) {
			d.processNode(n, scope)
		},
	}
	w.Elem(e)
}

// processNode processes a single adt Node; if it's a struct literal,
// it decorates both embedded and field-level attributes.
func (d *externDecorator) processNode(n adt.Node, scope *adt.Vertex) {
	s, ok := n.(*adt.StructLit)
	if !ok {
		return
	}
	kinds := d.fileKinds[s.Src.Pos().File()]
	// Process attributes on fields.
	for _, decl := range s.Decls {
		var valuePtr *adt.Expr
		switch decl := decl.(type) {
		case *adt.Field:
			valuePtr = &decl.Value
		case *adt.BulkOptionalField:
			valuePtr = &decl.Value
		case *adt.DynamicField:
			valuePtr = &decl.Value
		default:
			continue
		}
		srcField := decl.Source().(*ast.Field) // We know all the above types come from ast.Field.
		name, _, _ := ast.LabelName(srcField.Label)
		for _, attr := range srcField.Attrs {
			if expr := d.externValue(attr, name, kinds, scope); expr != nil {
				*valuePtr = &adt.BinaryExpr{
					Op: adt.AndOp,
					X:  *valuePtr,
					Y:  expr,
				}
			}
		}
	}

	// Process embedded attributes.
	var srcDecls []ast.Decl
	switch src := s.Src.(type) {
	case *ast.File:
		srcDecls = src.Decls
	case *ast.StructLit:
		srcDecls = src.Elts
	default:
		panic("unexpected type in adt.StructLit.Src")
	}
	for _, decl := range srcDecls {
		if attr, ok := decl.(*ast.Attribute); ok {
			if expr := d.externValue(attr, "", kinds, scope); expr != nil {
				s.Decls = append(s.Decls, expr)
			}
		}
	}
}

func (d *externDecorator) externValue(astAttr *ast.Attribute, name string, kinds map[string]bool, scope *adt.Vertex) adt.Expr {
	if !kinds[astAttr.Name()] {
		return nil
	}
	attr := internal.ParseAttr(astAttr)
	if attr.Err != nil {
		d.errs = errors.Append(d.errs, attr.Err)
		return nil
	}
	c := d.compilers[attr.Name]
	if c == nil {
		return nil
	}
	if a, ok, _ := attr.Lookup(1, "name"); ok {
		name = a
	}
	b, err := c.Compile(name, scope, attr)
	if err != nil {
		d.errs = errors.Append(d.errs, errors.Wrap(errors.Newf(attr.Pos, "@%s", attr.Name), err))
		return nil
	}
	return b
}
