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
	"iter"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/walk"
)

// SetInjection sets the injection value to be used for injection
// of values with an @extern(kind) attribute where kind is i.Kind().
func (r *Runtime) SetInjection(i Injection) {
	if r.injections == nil {
		r.injections = map[string]Injection{}
	}
	r.injections[i.Kind()] = i
}

// Injection defines an entrypoint for creating per-instance injectors.
type Injection interface {
	// InjectorForInstance returns a new injector for the
	// given build instance.
	InjectorForInstance(b *build.Instance, r *Runtime) (Injector, errors.Error)

	// Kind returns the @extern kind for this injection,
	// for example "embed" for @extern(embed).
	// A given Injection instance should always return
	// the same value.
	Kind() string
}

// An Injector fills in an adt.Expr for fields marked with `@extern(kind)`.
type Injector interface {
	// InjectedValue returns a value to be unified at the position of
	// the given external attribute. The scope argument
	// holds the value of the instance before any injections
	// have been unified into it.
	InjectedValue(attr *ExternAttr, scope *adt.Vertex) (adt.Expr, errors.Error)
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

// externDecorator locates extern attributes and calls the relevant injectors
// to inject values.
type externDecorator struct {
	runtime *Runtime
	pkg     *build.Instance

	injectors map[string]Injector

	// fileKinds maps each AST file to the extern kinds declared in it,
	// along with their file-level @extern attribute.
	fileKinds map[*token.File]map[string]*internal.Attr

	errs errors.Error
}

// addFile finds injection points in the given ast.File for external
// implementations.
func (d *externDecorator) addFile(f *ast.File) (errs errors.Error) {
	kinds, _, err := findExternFileAttrs(f)
	if err != nil {
		return err
	}
	if len(kinds) == 0 {
		return nil
	}

	if d.fileKinds == nil {
		d.fileKinds = map[*token.File]map[string]*internal.Attr{}
	}
	d.fileKinds[f.Pos().File()] = kinds

	for kind, attr := range kinds {
		if err := d.initInjector(kind, attr.Pos); err != nil {
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
func findExternFileAttrs(f *ast.File) (kinds map[string]*internal.Attr, decls []ast.Decl, err errors.Error) {
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
				kinds = map[string]*internal.Attr{}
			}
			if _, ok := kinds[k]; ok {
				err = errors.Append(err, errors.Newf(pos,
					"duplicate @extern attribute for kind %q", k))
				continue
			}
			kinds[k] = &attr
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

// initInjector initializes the injector for kind, if applicable. The pos
// argument represents the position of the file-level @extern attribute.
func (d *externDecorator) initInjector(kind string, pos token.Pos) errors.Error {
	if _, ok := d.injectors[kind]; ok {
		return nil
	}
	if d.injectors == nil {
		d.injectors = map[string]Injector{}
	}
	x := d.runtime.injections[kind]
	if x == nil {
		return errors.Newf(pos, "no interpreter defined for %q", kind)
	}
	inj, err := x.InjectorForInstance(d.pkg, d.runtime)
	if err != nil {
		return err
	}
	d.injectors[kind] = inj
	return nil
}

type ExternAttrs struct {
	// TopLevel holds all the extern attributes declared
	// before the package directive, e.g. @extern(embed)
	TopLevel map[string]*internal.Attr

	// Body holds a sequence of (node, attribute) pairs
	// corresponding to all the extern attributes in the body
	// of the file.
	Body iter.Seq[ExternAttr]
}

type ExternAttr struct {
	// TopLevel holds the top level @extern attribute for the attribute, for example @extern(embed).
	// This is the same as ExternAttrs.TopLevel[TopLevel.Name].
	TopLevel *internal.Attr

	// Parent holds the parent AST node that contains the attribute.
	// It's either a *ast.Field, *ast.StructLit, or *ast.File.
	Parent ast.Node

	// Attr holds the extern attribute itself.
	Attr *internal.Attr
}

func ExternAttrsForFile(file *ast.File) (*ExternAttrs, errors.Error) {
	kinds, decls, err := findExternFileAttrs(file)
	if err != nil {
		return nil, err
	}
	return &ExternAttrs{
		TopLevel: kinds,
		Body: func(yield func(ExternAttr) bool) {
			if len(kinds) > 0 {
				walkExternFileAttrs(file, decls, kinds, yield)
			}
		},
	}, nil
}

func walkExternFileAttrs(file *ast.File, decls []ast.Decl, kinds map[string]*internal.Attr, yield func(ExternAttr) bool) {
	ast.Walk(&ast.File{Decls: decls}, func(n ast.Node) bool {
		var elts []ast.Decl
		parent := n
		switch n := n.(type) {
		case *ast.StructLit:
			elts = n.Elts
		case *ast.File:
			parent = file
			elts = n.Decls
		default:
			return true
		}
		for _, elt := range elts {
			switch elt := elt.(type) {
			case *ast.Attribute:
				if !yieldAttr(elt, parent, kinds, yield) {
					return false
				}
			case *ast.Field:
				for _, attr := range elt.Attrs {
					if !yieldAttr(attr, elt, kinds, yield) {
						return false
					}
				}
			}
		}
		return true
	}, nil)
}

func yieldAttr(attr *ast.Attribute, parent ast.Node, kinds map[string]*internal.Attr, yield func(ExternAttr) bool) bool {
	name, body := attr.Split()
	toplevel := kinds[name]
	if toplevel == nil {
		return true
	}
	return yield(ExternAttr{
		TopLevel: toplevel,
		Parent:   parent,
		Attr:     ref(internal.ParseAttrBody(attr.Pos(), body)),
	})
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
		for _, attr := range srcField.Attrs {
			if expr := d.injectedValue(attr, srcField, kinds, scope); expr != nil {
				*valuePtr = &adt.BinaryExpr{
					Op: adt.AndOp,
					X:  *valuePtr,
					Y:  expr,
				}
			}
		}
	}

	// Process embedded attributes.
	var srcParent ast.Node
	var srcDecls []ast.Decl
	switch src := s.Src.(type) {
	case *ast.File:
		srcParent = src
		srcDecls = src.Decls
	case *ast.StructLit:
		srcParent = src
		srcDecls = src.Elts
	default:
		panic("unexpected type in adt.StructLit.Src")
	}
	for _, decl := range srcDecls {
		if attr, ok := decl.(*ast.Attribute); ok {
			if expr := d.injectedValue(attr, srcParent, kinds, scope); expr != nil {
				s.Decls = append(s.Decls, expr)
			}
		}
	}
}

func (d *externDecorator) injectedValue(attr *ast.Attribute, parent ast.Node, kinds map[string]*internal.Attr, scope *adt.Vertex) adt.Expr {
	kind, body := attr.Split()
	topLevel := kinds[kind]
	if topLevel == nil {
		return nil
	}
	parsed := internal.ParseAttrBody(attr.Pos(), body)
	if parsed.Err != nil {
		d.errs = errors.Append(d.errs, parsed.Err)
		return nil
	}
	inj := d.injectors[kind]
	if inj == nil {
		return nil
	}
	ea := &ExternAttr{
		TopLevel: topLevel,
		Parent:   parent,
		Attr:     &parsed,
	}
	b, err := inj.InjectedValue(ea, scope)
	if err != nil {
		d.errs = errors.Append(d.errs, errors.Wrap(errors.Newf(attr.Pos(), "@%s", kind), err))
		return nil
	}
	return b
}

func ref[T any](x T) *T {
	return &x
}
