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

package yaml

import (
	"errors"
	"io"

	"cuelang.org/go/cue/ast"
)

// ComposeFile assembles decoded YAML document expressions into a CUE
// file: the fields of a single mapping document become the file's
// declarations, any other single document is embedded, and a document
// stream becomes an embedded list.
func ComposeFile(filename string, a []ast.Expr) *ast.File {
	f := &ast.File{Filename: filename}
	switch len(a) {
	case 0:
	case 1:
		switch x := a[0].(type) {
		case *ast.StructLit:
			f.Decls = x.Elts
		default:
			f.Decls = []ast.Decl{&ast.EmbedDecl{Expr: x}}
		}
	default:
		f.Decls = []ast.Decl{&ast.EmbedDecl{Expr: &ast.ListLit{Elts: a}}}
	}
	return f
}

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
			if err != io.EOF {
				// Decode does not return errors in lenient mode, but
				// guard against it regardless.
				d.errs = append(d.errs, err)
			}
			if expr != nil {
				a = append(a, expr)
			}
			break
		}
		a = append(a, expr)
	}
	return ComposeFile(filename, a), errors.Join(d.errs...)
}
