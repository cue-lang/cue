// Copyright 2025 CUE Authors
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

package json

import (
	"fmt"
	"iter"
	"strconv"
	"strings"

	"cuelang.org/go/cue"
)

var (
	jsonPtrEsc   = strings.NewReplacer("~", "~0", "/", "~1")
	jsonPtrUnesc = strings.NewReplacer("~0", "~", "~1", "/")
)

// Pointer represents a JSON Pointer as defined by RFC 6901.
// It is a slash-separated list of tokens that reference a specific location
// within a JSON document.
// TODO(go1.26) alias this to [encoding/json/jsontext.Pointer]
type Pointer string

// PointerFromTokens returns a JSON Pointer formed from
// the unquoted tokens in the given sequence. Any
// slash (/) or tilde (~) characters will be escaped appropriately.
func PointerFromTokens(tokens iter.Seq[string]) Pointer {
	var buf strings.Builder
	for tok := range tokens {
		buf.WriteByte('/')
		buf.WriteString(jsonPtrEsc.Replace(tok))
	}
	return Pointer(buf.String())
}

// Tokens returns a sequence of all the
// unquoted path elements (tokens) of the JSON Pointer.
func (p Pointer) Tokens() iter.Seq[string] {
	s := string(p)
	return func(yield func(string) bool) {
		needUnesc := strings.IndexByte(s, '~') >= 0
		for len(s) > 0 {
			s = strings.TrimPrefix(s, "/")
			i := min(uint(strings.IndexByte(s, '/')), uint(len(s)))
			tok := s[:i]
			if needUnesc {
				tok = jsonPtrUnesc.Replace(tok)
			}
			if !yield(tok) {
				return
			}
			s = s[i:]
		}
	}
}

// PointerFromCUEPath returns a JSON Pointer equivalent to the
// given CUE path. It returns an error if the path contains an element
// that cannot be represented as a JSON Pointer.
func PointerFromCUEPath(p cue.Path) (Pointer, error) {
	var err error
	ptr := PointerFromTokens(func(yield func(s string) bool) {
		for _, sel := range p.Selectors() {
			var token string
			switch sel.Type() {
			case cue.StringLabel:
				token = sel.Unquoted()
			case cue.IndexLabel:
				token = strconv.Itoa(sel.Index())
			default:
				if err == nil {
					err = fmt.Errorf("cannot convert selector %v to JSON pointer", sel)
					continue
				}
			}
			if !yield(token) {
				return
			}
		}
	})
	if err != nil {
		return "", err
	}
	return ptr, nil
}
