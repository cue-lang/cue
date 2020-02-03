// Copyright 2019 CUE Authors
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
	"sort"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal"
)

// This file includes functionality for parsing attributes.
// These functions are slightly more permissive than the spec. Together with the
// scanner and parser the full spec is implemented, though.

// attributes is used to store per-key attribute text for a fields.
// It deliberately does not implement the value interface, as it should
// never act as a value in any way.
type attributes struct {
	attr []attr
}
type attr struct {
	text   string
	offset int
}

func (a *attr) key() string {
	return a.text[1:a.offset]
}

func (a *attr) body() string {
	return a.text[a.offset+1 : len(a.text)-1]
}

func createAttrs(ctx *context, src source, attrs []*ast.Attribute) (a *attributes, err *bottom) {
	if len(attrs) == 0 {
		return nil, nil
	}
	as := []attr{}
	for _, a := range attrs {
		index := strings.IndexByte(a.Text, '(')
		n := len(a.Text)
		if index < 2 || a.Text[0] != '@' || a.Text[n-1] != ')' {
			return nil, ctx.mkErr(newNode(a), "invalid attribute %q", a.Text)
		}
		as = append(as, attr{a.Text[:n], index})

		if err := internal.ParseAttrBody(src.Pos(), a.Text[index+1:n-1]).Err; err != nil {
			return nil, ctx.mkErr(newNode(a), err)
		}
	}

	sort.SliceStable(as, func(i, j int) bool { return as[i].text < as[j].text })
	// TODO: remove these restrictions.
	for i := 1; i < len(as); i++ {
		if ai, aj := as[i-1], as[i]; ai.key() == aj.key() {
			n := newNode(attrs[0])
			return nil, ctx.mkErr(n, "multiple attributes for key %q", ai.key())
		}
	}

	return &attributes{as}, nil
}

// unifyAttrs merges the attributes from a and b. It may return either a or b
// if a and b are identical.
func unifyAttrs(ctx *context, src source, a, b *attributes) (atrs *attributes, err evaluated) {
	if a == b {
		return a, nil
	}
	if a == nil {
		return b, nil
	}
	if b == nil {
		return a, nil
	}

	if len(a.attr) == len(b.attr) {
		for i, x := range a.attr {
			if x != b.attr[i] {
				goto notSame
			}
		}
		return a, nil
	}

notSame:
	as := append(a.attr, b.attr...)

	// remove duplicates and error on conflicts
	sort.Slice(as, func(i, j int) bool { return as[i].text < as[j].text })
	k := 0
	for i := 1; i < len(as); i++ {
		if ak, ai := as[k], as[i]; ak.key() == ai.key() {
			if ak.body() == ai.body() {
				continue
			}
			return nil, ctx.mkErr(src, "conflicting attributes for key %q", ai.key())
		}
		k++
		as[k] = as[i]
	}

	return &attributes{as[:k+1]}, nil
}

// parsedAttr holds positional information for a single parsedAttr.
type parsedAttr struct {
	fields []keyValue
}

type keyValue struct {
	data  string
	equal int // index of equal sign or 0 if non-existing
}

func (kv *keyValue) text() string  { return kv.data }
func (kv *keyValue) key() string   { return kv.data[:kv.equal] }
func (kv *keyValue) value() string { return kv.data[kv.equal+1:] }
