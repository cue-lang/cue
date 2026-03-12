// Copyright 2025 The CUE Authors
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

package koala

import (
	"bytes"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	koalaenc "cuelang.org/go/encoding/xml/koala"
)

// Marshal returns the koala XML encoding of v.
func Marshal(v cue.Value) (string, error) {
	if err := v.Validate(cue.Concrete(true)); err != nil {
		return "", err
	}
	var b strings.Builder
	enc := koalaenc.NewEncoder(&b)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return b.String(), nil
}

// Unmarshal parses koala XML data to a CUE expression.
func Unmarshal(data []byte) (ast.Expr, error) {
	return koalaenc.NewDecoder("", bytes.NewReader(data)).Decode()
}
