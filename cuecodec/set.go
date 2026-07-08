// Copyright 2026 The CUE Authors
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

package cuecodec

import "strings"

// A Set is an immutable collection of codecs. It maps format names and
// file extensions to their codecs.
//
// TODO(v2): a Set will also hold interpreters once the cue/v2 value API
// exists.
type Set struct {
	// list holds the elements in insertion order; later elements win
	// when two claim the same extension.
	list   []Elem
	byName map[string]Elem
	byExt  map[string]Codec
}

// An Elem is a member of a Set. For now the only kind of element is a
// [Codec]; interpreters will be added with the cue/v2 value API.
type Elem interface {
	Name() string
}

// Default returns the default set: the cue, json, and yaml codecs.
func Default() *Set {
	return NewSet(CUE(), JSON(), YAML())
}

// NewSet returns a set of exactly the given elements. When two elements
// share a name, the later one wins; likewise for extensions.
func NewSet(elems ...Elem) *Set {
	s := &Set{}
	for _, e := range elems {
		s.add(e)
	}
	return s
}

// With returns a set extending s with the given elements; elements with
// the same name replace existing ones. The receiver is not modified.
func (s *Set) With(elems ...Elem) *Set {
	ns := &Set{list: append([]Elem(nil), s.list...)}
	for _, e := range elems {
		ns.add(e)
	}
	return ns
}

// add mutates s to include e, replacing any existing element of the
// same name in place. It is used only while constructing a fresh Set.
func (s *Set) add(e Elem) {
	replaced := false
	for i, existing := range s.list {
		if existing.Name() == e.Name() {
			s.list[i] = e
			replaced = true
			break
		}
	}
	if !replaced {
		s.list = append(s.list, e)
	}
	s.rebuild()
}

// rebuild recomputes the name and extension indexes from list.
func (s *Set) rebuild() {
	s.byName = make(map[string]Elem, len(s.list))
	s.byExt = make(map[string]Codec)
	for _, e := range s.list {
		s.byName[e.Name()] = e
		if c, ok := e.(Codec); ok {
			for _, ext := range c.Extensions() {
				s.byExt[normalizeExt(ext)] = c
			}
		}
	}
}

// Lookup returns the named element, if present.
func (s *Set) Lookup(name string) (Elem, bool) {
	e, ok := s.byName[name]
	return e, ok
}

// ByExtension returns the codec that claims the given file extension.
// The extension may be given with or without its leading dot.
func (s *Set) ByExtension(ext string) (Codec, bool) {
	c, ok := s.byExt[normalizeExt(ext)]
	return c, ok
}

func normalizeExt(ext string) string {
	if ext == "" || strings.HasPrefix(ext, ".") {
		return ext
	}
	return "." + ext
}
