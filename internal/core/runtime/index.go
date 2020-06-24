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

package runtime

import (
	"reflect"
	"sync"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
)

// Index maps conversions from label names to internal codes.
//
// All instances belonging to the same package should share this Index.
//
// INDEX IS A TRANSITIONAL TYPE TO BRIDGE THE OLD AND NEW
// IMPLEMENTATIONS. USE RUNTIME.
type Index struct {
	labelMap map[string]int64
	labels   []string

	// Change this to Instance at some point.
	// From *structLit/*Vertex -> Instance
	imports       map[interface{}]interface{}
	importsByPath map[string]interface{}
	// imports map[string]*adt.Vertex

	offset int64
	parent *Index

	// mutex     sync.Mutex
	typeCache sync.Map // map[reflect.Type]evaluated
}

// SharedIndex is used for indexing builtins and any other labels common to
// all instances.
var SharedIndex = newSharedIndex()

var SharedIndexNew = newSharedIndex()

var SharedRuntimeNew = &Runtime{index: SharedIndexNew}

func newSharedIndex() *Index {
	i := &Index{
		labelMap:      map[string]int64{"": 0},
		labels:        []string{""},
		imports:       map[interface{}]interface{}{},
		importsByPath: map[string]interface{}{},
	}
	return i
}

// NewIndex creates a new index.
func NewIndex(parent *Index) *Index {
	i := &Index{
		labelMap:      map[string]int64{},
		imports:       map[interface{}]interface{}{},
		importsByPath: map[string]interface{}{},
		offset:        int64(len(parent.labels)) + parent.offset,
		parent:        parent,
	}
	return i
}

func (x *Index) IndexToString(i int64) string {
	for ; i < x.offset; x = x.parent {
	}
	return x.labels[i-x.offset]
}

func (x *Index) StringToIndex(s string) int64 {
	for p := x; p != nil; p = p.parent {
		if f, ok := p.labelMap[s]; ok {
			return int64(f)
		}
	}
	index := int64(len(x.labelMap)) + x.offset
	x.labelMap[s] = index
	x.labels = append(x.labels, s)
	return int64(index)
}

func (x *Index) HasLabel(s string) (ok bool) {
	for c := x; c != nil; c = c.parent {
		_, ok = c.labelMap[s]
		if ok {
			break
		}
	}
	return ok
}

func (x *Index) StoreType(t reflect.Type, v interface{}) {
	x.typeCache.Store(t, v)
}

func (x *Index) LoadType(t reflect.Type) (v interface{}, ok bool) {
	v, ok = x.typeCache.Load(t)
	return v, ok
}

func (x *Index) StrLabel(str string) adt.Feature {
	return x.Label(str, false)
}

func (x *Index) NodeLabel(n ast.Node) (f adt.Feature, ok bool) {
	switch label := n.(type) {
	case *ast.BasicLit:
		name, _, err := ast.LabelName(label)
		return x.Label(name, false), err == nil
	case *ast.Ident:
		name, err := ast.ParseIdent(label)
		return x.Label(name, true), err == nil
	}
	return 0, false
}

func (x *Index) Label(s string, isIdent bool) adt.Feature {
	index := x.StringToIndex(s)
	typ := adt.StringLabel
	if isIdent {
		switch {
		case internal.IsDef(s) && internal.IsHidden(s):
			typ = adt.HiddenDefinitionLabel
		case internal.IsDef(s):
			typ = adt.DefinitionLabel
		case internal.IsHidden(s):
			typ = adt.HiddenLabel
		}
	}
	f, _ := adt.MakeLabel(nil, index, typ)
	return f
}

func (idx *Index) LabelStr(l adt.Feature) string {
	index := int64(l.Index())
	return idx.IndexToString(index)
}

func (x *Index) AddInst(path string, key, p interface{}) {
	if key == nil {
		panic("key must not be nil")
	}
	x.imports[key] = p
	if path != "" {
		x.importsByPath[path] = key
	}
}

func (x *Index) GetImportFromNode(key interface{}) interface{} {
	imp := x.imports[key]
	if imp == nil && x.parent != nil {
		return x.parent.GetImportFromNode(key)
	}
	return imp
}

func (x *Index) GetImportFromPath(id string) interface{} {
	key := x.importsByPath[id]
	if key == nil && x.parent != nil {
		return x.parent.GetImportFromPath(id)
	}
	return key
}
