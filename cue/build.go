// Copyright 2018 The CUE Authors
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

package cue

import (
	"path"
	"strconv"

	"cuelang.org/go/cue/ast"
	build "cuelang.org/go/cue/build"
	"cuelang.org/go/cue/token"
)

// Build creates one Instance for each build.Instance. A returned Instance
// may be incomplete, in which case its Err field is set.
//
// Example:
//	inst := cue.Build(load.Load(args))
//
func Build(instances []*build.Instance) []*Instance {
	if len(instances) == 0 {
		panic("cue: list of instances must not be empty")
	}
	index := newIndex(instances[0].Context().FileSet())

	loaded := []*Instance{}

	for _, p := range instances {
		p.Complete()

		loaded = append(loaded, index.loadInstance(p))
	}
	// TODO: insert imports
	return loaded
}

// FromExpr creates anb instance from an expression.
// Any references must be resolved beforehand.
func FromExpr(fset *token.FileSet, expr ast.Expr) (*Instance, error) {
	i := newIndex(fset).NewInstance(nil)
	err := i.insertFile(&ast.File{
		Decls: []ast.Decl{&ast.EmitDecl{Expr: expr}},
	})
	if err != nil {
		return nil, err
	}
	return i, nil
}

// index maps conversions from label names to internal codes.
//
// All instances belonging to the same package should share this index.
type index struct {
	fset *token.FileSet

	labelMap map[string]label
	labels   []string

	loaded map[*build.Instance]*Instance

	offset label
	parent *index
	freeze bool
}

// sharedIndex is used for indexing builtins and any other labels common to
// all instances.
var sharedIndex = newSharedIndex(token.NewFileSet())

func newSharedIndex(f *token.FileSet) *index {
	i := &index{
		fset:     f,
		labelMap: map[string]label{"": 0},
		labels:   []string{""},
	}
	return i
}

// newIndex creates a new index.
func newIndex(f *token.FileSet) *index {
	parent := sharedIndex
	i := &index{
		fset:     f,
		labelMap: map[string]label{},
		loaded:   map[*build.Instance]*Instance{},
		offset:   label(len(parent.labels)) + parent.offset,
		parent:   parent,
	}
	parent.freeze = true
	return i
}

func (idx *index) strLabel(str string) label {
	return idx.label(str, false)
}

func (idx *index) nodeLabel(n ast.Node) (f label, ok bool) {
	switch x := n.(type) {
	case *ast.BasicLit:
		name, ok := ast.LabelName(x)
		return idx.label(name, false), ok
	case *ast.Ident:
		return idx.label(x.Name, true), true
	}
	return 0, false
}

func (idx *index) findLabel(s string) (f label, ok bool) {
	for x := idx; x != nil; x = x.parent {
		f, ok = x.labelMap[s]
		if ok {
			break
		}
	}
	return f, ok
}

func (idx *index) label(s string, isIdent bool) label {
	f, ok := idx.findLabel(s)
	if !ok {
		if idx.freeze {
			panic("adding label to frozen index")
		}
		f = label(len(idx.labelMap)) + idx.offset
		idx.labelMap[s] = f
		idx.labels = append(idx.labels, s)
	}
	f <<= 1
	if isIdent && s != "" && s[0] == '_' {
		f |= 1
	}
	return f
}

func (idx *index) labelStr(l label) string {
	l >>= 1
	for ; l < idx.offset; idx = idx.parent {
	}
	return idx.labels[l-idx.offset]
}

func (idx *index) loadInstance(p *build.Instance) *Instance {
	if inst := idx.loaded[p]; inst != nil {
		if !inst.complete {
			// cycles should be detected by the builder and it should not be
			// possible to construct a build.Instance that has them.
			panic("cue: cycle")
		}
		return inst
	}
	files := p.Files
	inst := idx.NewInstance(p)
	if inst.Err == nil {
		// inst.instance.index.state = s
		// inst.instance.inst = p
		inst.Err = resolveFiles(idx, p)
		for _, f := range files {
			inst.insertFile(f)
		}
	}
	inst.complete = true
	return inst
}

func lineStr(idx *index, n ast.Node) string {
	return idx.fset.Position(n.Pos()).String()
}

func resolveFiles(idx *index, p *build.Instance) error {
	// Link top-level declarations. As top-level entries get unified, an entry
	// may be linked to any top-level entry of any of the files.
	allFields := map[string]ast.Node{}
	for _, file := range p.Files {
		for _, d := range file.Decls {
			if f, ok := d.(*ast.Field); ok && f.Value != nil {
				if ident, ok := f.Label.(*ast.Ident); ok {
					allFields[ident.Name] = f.Value
				}
			}
		}
	}
	for _, f := range p.Files {
		if err := resolveFile(idx, f, p, allFields); err != nil {
			return err
		}
	}
	return nil
}

func resolveFile(idx *index, f *ast.File, p *build.Instance, allFields map[string]ast.Node) error {
	type importInfo struct {
		node ast.Node
		inst *Instance
		used bool // TODO: use a more general unresolved value technique
	}
	index := map[string][]*ast.Ident{}
	for _, u := range f.Unresolved {
		index[u.Name] = append(index[u.Name], u)
	}
	fields := map[string]ast.Node{}
	for _, d := range f.Decls {
		if f, ok := d.(*ast.Field); ok && f.Value != nil {
			if ident, ok := f.Label.(*ast.Ident); ok {
				fields[ident.Name] = d
			}
		}
	}
	var errUnused error

	for _, spec := range f.Imports {
		id, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue // quietly ignore the error
		}
		name := path.Base(id)
		if imp := p.LookupImport(id); imp != nil {
			name = imp.PkgName
			if spec.Name != nil {
				name = spec.Name.Name
			}
		} else if _, ok := builtins[id]; !ok {
			// continue
			return idx.mkErr(newNode(spec), "package %q not found", id)
		}
		if n, ok := fields[name]; ok {
			return idx.mkErr(newNode(spec),
				"%s redeclared as imported package name\n"+
					"\tprevious declaration at %v", name, lineStr(idx, n))
		}
		used := false
		for _, u := range index[name] {
			used = true
			u.Node = spec
		}
		if !used {
			if spec.Name == nil {
				errUnused = idx.mkErr(newNode(spec),
					"imported and not used: %s", spec.Path.Value)
			} else {
				errUnused = idx.mkErr(newNode(spec),
					"imported and not used: %s as %s", spec.Path.Value, spec.Name)
			}
		}
	}
	i := 0
	for _, u := range f.Unresolved {
		if u.Node != nil {
			continue
		}
		if n, ok := allFields[u.Name]; ok {
			u.Node = n
			u.Scope = f
			continue
		}
		f.Unresolved[i] = u
		i++
	}
	f.Unresolved = f.Unresolved[:i]
	// TODO: also need to resolve types.
	// if len(f.Unresolved) > 0 {
	// 	n := f.Unresolved[0]
	// 	return ctx.mkErr(newBase(n), "unresolved reference %s", n.Name)
	// }
	if errUnused != nil {
		return errUnused
	}
	return nil
}
