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
		fileAttrs []*ast.Attribute
	)

loop:
	for ; p < len(f.Decls); p++ {
		switch a := f.Decls[p].(type) {
		case *ast.Package:
			hasPkg = true
			break loop

		case *ast.Attribute:
			pos := a.Pos()
			key, body := a.Split()
			if key != "extern" {
				continue
			}
			fileAttrs = append(fileAttrs, a)

			attr := internal.ParseAttrBody(pos, body)
			if attr.Err != nil {
				err = errors.Append(err, attr.Err)
				continue
			}
			k, e := attr.String(0)
			if e != nil {
				// Unreachable.
				err = errors.Append(err, errors.Newf(pos, "%s", e))
				continue
			}

			if k == "" {
				err = errors.Append(err, errors.Newf(pos,
					"interpreter name must be non-empty"))
				continue
			}

			if kinds == nil {
				kinds = map[string]token.Pos{}
			}
			if _, ok := kinds[k]; ok {
				err = errors.Append(err, errors.Newf(pos,
					"duplicate @extern attribute for kind %q", k))
				continue
			}
			kinds[k] = pos
		}
	}

	switch {
	case len(fileAttrs) == 0 && !hasPkg:
		return nil, nil, err

	case len(fileAttrs) > 0 && !hasPkg:
		for _, a := range fileAttrs {
			err = errors.Append(err, errors.Newf(a.Pos(),
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

// ExtractFieldAttrsByKind finds all the attributes of the given kind
// in the given AST, parsing their bodies into [internal.Attr].
// TODO this API does not fit the way that extern attributes can now
// be used; in particular, there is no longer necessarily a field associated
// with every extern attribute.
func ExtractFieldAttrsByKind(file *ast.File, kind string) (attrsByField map[*ast.Field]*internal.Attr, errs errors.Error) {
	kinds, decls, err := findExternFileAttrs(file)
	if err != nil || len(decls) == 0 {
		return nil, err
	}
	if _, ok := kinds[kind]; !ok {
		return nil, nil
	}

	var fieldStack []*ast.Field

	ast.Walk(&ast.File{Decls: decls}, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Field:
			fieldStack = append(fieldStack, n)

		case *ast.Attribute:
			pos := n.Pos()
			k, body := n.Split()

			// Support old-style and new-style extern attributes.
			if k != "extern" && k != kind {
				break
			}

			lastFieldIdx := len(fieldStack) - 1
			if lastFieldIdx < 0 {
				errs = errors.Append(errs, errors.Newf(pos, "@%s attribute not associated with field", kind))
				return true
			}

			f := fieldStack[lastFieldIdx]

			_, _, err := ast.LabelName(f.Label)
			if err != nil {
				b, _ := format.Node(f.Label)
				errs = errors.Append(errs, errors.Newf(pos, "external attribute has non-concrete label %s", b))
				break
			}

			if attrsByField == nil {
				attrsByField = make(map[*ast.Field]*internal.Attr)
			}
			if _, found := attrsByField[f]; found {
				errs = errors.Append(errs, errors.Newf(pos, "duplicate @%s attributes", k))
				break
			}

			attrParsed := internal.ParseAttrBody(pos, body)
			attrsByField[f] = &attrParsed

			return false
		}

		return true

	}, func(n ast.Node) {
		switch n.(type) {
		case *ast.Field:
			fieldStack = fieldStack[:len(fieldStack)-1]
		}
	})

	return attrsByField, errs
}

func (d *externDecorator) decorateConjunct(e adt.Elem, scope *adt.Vertex) {
	// Note: we want to apply the fixups to the tree _after_
	// invoking [walk.Visitor.Elem] so that we can avoid
	// the need to walk the nodes of the external value too.
	var fixups []func()
	w := walk.Visitor{Before: func(n adt.Node) bool {
		if s, ok := n.(*adt.StructLit); ok {
			d.processStructLit(s, scope, func(f func()) {
				fixups = append(fixups, f)
			})
		}
		return true
	}}
	w.Elem(e)
	for _, f := range fixups {
		f()
	}
}

// processStructLit processes a single StructLit, handling both field-level
// and embedded extern attributes.
func (d *externDecorator) processStructLit(s *adt.StructLit, scope *adt.Vertex, fixup func(func())) bool {
	if s.Src == nil {
		return false
	}
	kinds := d.fileKinds[s.Src.Pos().File()]
	if len(kinds) == 0 {
		// No extern interpreters enabled in this file: no need
		// to walk any nodes inside it.
		return false
	}
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
				fixup(func() {
					*valuePtr = &adt.BinaryExpr{
						Op: adt.AndOp,
						X:  *valuePtr,
						Y:  expr,
					}
				})
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
				fixup(func() {
					s.Decls = append(s.Decls, expr)
				})
			}
		}
	}
	return true
}

func (d *externDecorator) externValue(attr *ast.Attribute, name string, kinds map[string]bool, scope *adt.Vertex) adt.Expr {
	kind, body := attr.Split()
	if !kinds[kind] {
		return nil
	}
	parsed := internal.ParseAttrBody(attr.Pos(), body)
	if parsed.Err != nil {
		d.errs = errors.Append(d.errs, parsed.Err)
		return nil
	}
	c := d.compilers[kind]
	if c == nil {
		return nil
	}
	if a, ok, _ := parsed.Lookup(1, "name"); ok {
		name = a
	}
	b, err := c.Compile(name, scope, &parsed)
	if err != nil {
		d.errs = errors.Append(d.errs, errors.Wrap(errors.Newf(attr.Pos(), "@%s", kind), err))
		return nil
	}
	return b
}
