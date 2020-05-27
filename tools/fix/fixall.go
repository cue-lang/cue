// Copyright 2020 CUE Authors
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

package fix

import (
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
)

// Instances modifies all files contained in the given build instances at once.
//
// It also applies fix.File.
func Instances(a []*build.Instance) errors.Error {
	cwd, _ := os.Getwd()

	// Collect all
	p := processor{
		instances: a,
		cwd:       cwd,

		done:      map[ast.Node]bool{},
		rename:    map[*ast.Ident]string{},
		ambiguous: map[string][]token.Pos{},
	}

	p.visitAll(func(f *ast.File) { File(f) })

	instances := cue.Build(a)
	p.updateValues(instances)
	p.visitAll(p.tagAmbiguous)
	p.rewriteIdents()
	p.visitAll(p.renameFields)

	return p.err
}

type processor struct {
	instances []*build.Instance
	cwd       string

	done map[ast.Node]bool
	// Evidence for rewrites. Rewrite in a later pass.
	rename    map[*ast.Ident]string
	ambiguous map[string][]token.Pos

	stack []cue.Value

	err errors.Error
}

func (p *processor) updateValues(instances []*cue.Instance) {
	for _, inst := range instances {
		inst.Value().Walk(p.visit, nil)
	}
}

func (p *processor) visit(v cue.Value) bool {
	if e, ok := v.Elem(); ok {
		p.updateValue(e)
		p.visit(e)
	}

	if v.Kind() != cue.StructKind {
		p.updateValue(v)
		return true
	}

	p.stack = append(p.stack, v)
	defer func() { p.stack = p.stack[:len(p.stack)-1] }()

	for it, _ := v.Fields(cue.All()); it.Next(); {
		p.updateValue(it.Value())
		p.visit(it.Value())
	}

	for _, kv := range v.BulkOptionals() {
		p.updateValue(kv[0])
		p.visit(kv[0])
		p.updateValue(kv[1])
		p.visit(kv[1])
	}

	return false
}

func (p *processor) updateValue(v cue.Value) cue.Value {
	switch op, a := v.Expr(); op {
	case cue.NoOp:
		return v

	case cue.SelectorOp:
		v := p.updateValue(a[0])

		switch x := a[1].Source().(type) {
		case *ast.SelectorExpr:
			return p.lookup(v, x.Sel)

		case *ast.Ident:
			v := p.updateValue(a[0])
			return p.lookup(v, x)
		}

	default:
		for _, v := range a {
			p.updateValue(v)
		}
	}
	return v
}

func (p *processor) lookup(v cue.Value, l ast.Expr) cue.Value {
	label, ok := l.(ast.Label)
	if !ok {
		return cue.Value{}
	}

	name, isIdent, err := ast.LabelName(label)
	if err != nil {
		return cue.Value{}
	}
	f, err := v.FieldByName(name, isIdent)
	if err != nil {
		f := v.Template()
		if f == nil {
			return cue.Value{}
		}
		return v.Template()(name)
	}

	switch {
	case !p.done[l]:
		p.done[l] = true

		if !f.IsDefinition {
			break
		}

		if !ast.IsValidIdent(name) {
			p.err = errors.Append(p.err, errors.Newf(
				l.Pos(),
				"cannot convert reference to definition with invalid identifier %q",
				name))
			break
		}

		if ident, ok := l.(*ast.Ident); ok && !internal.IsDef(name) {
			p.rename[ident] = "#" + name
		}
	}

	return f.Value
}

// tagAmbiguous marks identifier fields were not handled by the previous pass.
// These can be identifiers within unused templates, for instance. It is
// possible to do further resolution within templates, but for now we will
// punt on this.
func (p *processor) tagAmbiguous(f *ast.File) {
	ast.Walk(f, p.tagRef, nil)
}

func (p *processor) tagRef(n ast.Node) bool {
	switch x := n.(type) {
	case *ast.Field:
		ast.Walk(x.Value, p.tagRef, nil)

		lab := x.Label
		if a, ok := x.Label.(*ast.Alias); ok {
			lab, _ = a.Expr.(ast.Label)
		}

		switch lab.(type) {
		case *ast.Ident, *ast.BasicLit:
		default: // list, paren, or interpolation
			ast.Walk(lab, p.tagRef, nil)
		}

		return false

	case *ast.Ident:
		if _, ok := p.done[x]; !ok {
			p.ambiguous[x.Name] = append(p.ambiguous[x.Name], x.Pos())
		}
		return false
	}
	return true
}

func (p *processor) rewriteIdents() {
	for x, name := range p.rename {
		x.Name = name
	}
}

func (p *processor) renameFields(f *ast.File) {
	hasErr := false
	_ = astutil.Apply(f, func(c astutil.Cursor) bool {
		switch x := c.Node().(type) {
		case *ast.Field:
			if x.Token != token.ISA {
				return true
			}

			label, isIdent, err := ast.LabelName(x.Label)
			if err != nil {
				b, _ := format.Node(x.Label)
				hasErr = true
				p.err = errors.Append(p.err, errors.Newf(x.Pos(),
					`cannot convert dynamic definition for '%s'`, string(b)))
				return false
			}

			if !isIdent && !ast.IsValidIdent(label) {
				hasErr = true
				p.err = errors.Append(p.err, errors.Newf(x.Pos(),
					`invalid identifier %q; definition must be valid label`, label))
				return false
			}

			if refs, ok := p.ambiguous[label]; ok {
				h := fnv.New32()
				_, _ = h.Write([]byte(label))
				opt := fmt.Sprintf("@tmpNoExportNewDef(%x)", h.Sum32()&0xffff)

				f := &ast.Field{
					Label: ast.NewIdent(label),
					Value: ast.NewIdent("#" + label),
					Attrs: []*ast.Attribute{{Text: opt}},
				}
				c.InsertAfter(f)

				b := &strings.Builder{}
				fmt.Fprintln(b, "Possible references to this location:")
				for _, r := range refs {
					s, err := filepath.Rel(p.cwd, r.String())
					if err != nil {
						s = r.String()
					}
					s = filepath.ToSlash(s)
					fmt.Fprintf(b, "\t%s\n", s)
				}

				cg := internal.NewComment(true, b.String())
				astutil.CopyPosition(cg, c.Node())
				ast.AddComment(c.Node(), cg)
			}

			x.Label = ast.NewIdent("#" + label)
			x.Token = token.COLON
		}

		return true
	}, nil)

	if hasErr {
		p.err = errors.Append(p.err, errors.Newf(token.NoPos, `Incompatible definitions detected:

A trick that can be used is to rename this to a regular identifier and then
move the definition to a sub field. For instance, rewrite

			"foo-bar" :: baz
			"foo\(bar)" :: baz

		to

			#defmap: "foo-bar": baz
			#defmap: "foo\(bar)": baz

Errors:`))
	}
}

func (p *processor) visitAll(fn func(f *ast.File)) {
	if p.err != nil {
		return
	}

	done := map[*ast.File]bool{}

	for _, b := range p.instances {
		for _, f := range b.Files {
			if done[f] {
				continue
			}
			done[f] = true
			fn(f)
		}
	}
}
