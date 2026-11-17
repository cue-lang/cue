// Copyright 2026 CUE Authors
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

package goccy

import (
	"errors"
	"io"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/internal/encoding/yaml/yamlbase"
)

// ExtractLenient converts YAML in b to an equivalent CUE file while
// tolerating invalid YAML. Like [cuelang.org/go/cue/parser.ParseFile]
// it returns a best-effort, never nil file alongside any errors
// encountered: a document with a syntax error contributes the longest
// token prefix preceding the error that parses on its own, and any
// following documents are still decoded. This suits editor tooling
// handling files mid-edit; [cuelang.org/go/encoding/yaml.Extract] is
// the strict equivalent.
func ExtractLenient(filename string, b []byte) (*ast.File, error) {
	d := NewDecoder(filename, b)
	d.lenient = true
	var a []ast.Expr
	for {
		expr, err := d.Decode()
		if err != nil {
			// Decode never returns an expression alongside an error,
			// and in lenient mode the only error is io.EOF; guard
			// against others regardless.
			if err != io.EOF {
				d.errs = append(d.errs, err)
			}
			break
		}
		a = append(a, expr)
	}
	return yamlbase.ComposeFile(filename, a), errors.Join(d.errs...)
}
