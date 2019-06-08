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
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/token"
)

// A Runtime is used for creating CUE interpretations.
//
// Any operation that involves two Values or Instances should originate from
// the same Runtime.
type Runtime struct {
	Context *build.Context
	idx     *index
	ctxt    *build.Context
}

func dummyLoad(token.Pos, string) *build.Instance { return nil }

func (r *Runtime) index() *index {
	if r.idx == nil {
		r.idx = newIndex()
	}
	return r.idx
}

func (r *Runtime) complete(p *build.Instance) (*Instance, error) {
	idx := r.index()
	if err := p.Complete(); err != nil {
		return nil, err
	}
	inst := idx.loadInstance(p)
	if inst.Err != nil {
		return nil, inst.Err
	}
	return inst, nil
}

// Parse parses a CUE source value into a CUE Instance. The source code may
// be provided as a string, byte slice, or io.Reader. The name is used as the
// file name in position information. The source may import builtin packages.
//
func (r *Runtime) Parse(name string, source interface{}) (*Instance, error) {
	ctx := r.Context
	if ctx == nil {
		ctx = build.NewContext()
	}
	p := ctx.NewInstance(name, dummyLoad)
	if err := p.AddFile(name, source); err != nil {
		return nil, err
	}
	return r.complete(p)
}

// Build creates an Instance from the given build.Instance. A returned Instance
// may be incomplete, in which case its Err field is set.
func (r *Runtime) Build(instance *build.Instance) (*Instance, error) {
	return r.complete(instance)
}

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
	var r Runtime
	index := r.index()

	loaded := []*Instance{}

	for _, p := range instances {
		p.Complete()

		loaded = append(loaded, index.loadInstance(p))
	}
	// TODO: insert imports
	return loaded
}

// FromExpr creates an instance from an expression.
// Any references must be resolved beforehand.
func (r *Runtime) FromExpr(expr ast.Expr) (*Instance, error) {
	i := r.index().NewInstance(nil)
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
	labelMap map[string]label
	labels   []string

	loaded map[*build.Instance]*Instance

	offset label
	parent *index
	freeze bool
}

const sharedOffset = 0x40000000

// sharedIndex is used for indexing builtins and any other labels common to
// all instances.
var sharedIndex = newSharedIndex()

func newSharedIndex() *index {
	// TODO: nasty hack to indicate FileSet of shared index. Remove the whole
	// FileSet idea from the API. Just take the hit of the extra pointers for
	// positions in the ast, and then optimize the storage in an abstract
	// machine implementation for storing graphs.
	token.NewFile("dummy", sharedOffset, 0)
	i := &index{
		labelMap: map[string]label{"": 0},
		labels:   []string{""},
	}
	return i
}

// newIndex creates a new index.
func newIndex() *index {
	parent := sharedIndex
	i := &index{
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
	return n.Pos().String()
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
