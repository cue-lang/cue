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
	"cuelang.org/go/cue/literal"
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

func createAttrs(ctx *context, src source, attrs []*ast.Attribute) (a *attributes, err evaluated) {
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
	}

	sort.Slice(as, func(i, j int) bool { return as[i].text < as[j].text })
	for i := 1; i < len(as); i++ {
		if ai, aj := as[i-1], as[i]; ai.key() == aj.key() {
			n := newNode(attrs[0])
			return nil, ctx.mkErr(n, "multiple attributes for key %q", ai.key())
		}
	}

	for _, a := range attrs {
		if err := parseAttrBody(ctx, src, a.Text, nil); err != nil {
			return nil, err
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

func parseAttrBody(ctx *context, src source, s string, a *parsedAttr) (err evaluated) {
	i := 0
	for {
		// always scan at least one, possibly empty element.
		n, err := scanAttributeElem(ctx, src, s[i:], a)
		if err != nil {
			return err
		}
		if i += n; i >= len(s) {
			break
		}
		if s[i] != ',' {
			return ctx.mkErr(src, "invalid attribute: expected comma")
		}
		i++
	}
	return nil
}

func scanAttributeElem(ctx *context, src source, s string, a *parsedAttr) (n int, err evaluated) {
	// try CUE string
	kv := keyValue{}
	if n, kv.data, err = scanAttributeString(ctx, src, s); n == 0 {
		// try key-value pair
		p := strings.IndexAny(s, ",=") // ) is assumed to be stripped.
		switch {
		case p < 0:
			kv.data = s
			n = len(s)

		default: // ','
			n = p
			kv.data = s[:n]

		case s[p] == '=':
			kv.equal = p
			offset := p + 1
			var str string
			if p, str, err = scanAttributeString(ctx, src, s[offset:]); p > 0 {
				n = offset + p
				kv.data = s[:offset] + str
			} else {
				n = len(s)
				if p = strings.IndexByte(s[offset:], ','); p >= 0 {
					n = offset + p
				}
				kv.data = s[:n]
			}
		}
	}
	if a != nil {
		a.fields = append(a.fields, kv)
	}
	return n, err
}

func scanAttributeString(ctx *context, src source, s string) (n int, str string, err evaluated) {
	if s == "" || (s[0] != '#' && s[0] != '"' && s[0] != '\'') {
		return 0, "", nil
	}

	nHash := 0
	for {
		if nHash < len(s) {
			if s[nHash] == '#' {
				nHash++
				continue
			}
			if s[nHash] == '\'' || s[nHash] == '"' {
				break
			}
		}
		return nHash, s[:nHash], ctx.mkErr(src, "invalid attribute string")
	}

	// Determine closing quote.
	nQuote := 1
	if c := s[nHash]; nHash+6 < len(s) && s[nHash+1] == c && s[nHash+2] == c {
		nQuote = 3
	}
	close := s[nHash:nHash+nQuote] + s[:nHash]

	// Search for closing quote.
	index := strings.Index(s[len(close):], close)
	if index == -1 {
		return len(s), "", ctx.mkErr(src, "attribute string not terminated")
	}

	index += 2 * len(close)
	s, err2 := literal.Unquote(s[:index])
	if err2 != nil {
		return index, "", ctx.mkErr(src, "invalid attribute string: %v", err2)
	}
	return index, s, nil
}
