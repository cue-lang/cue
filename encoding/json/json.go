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

// Package yaml converts JSON to and from CUE.
package json

import (
	"bytes"
	gojson "encoding/json"
	"io"
	"strconv"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/ast/astutil"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/source"
	"cuelang.org/go/pkg/encoding/json"
)

// Valid reports whether data is a valid JSON encoding.
func Valid(b []byte) bool {
	return gojson.Valid(b)
}

// Validate validates JSON and confirms it matches the constraints
// specified by v.
func Validate(b []byte, v cue.Value) error {
	_, err := json.Validate(b, v)
	return err
}

// Extract parses the YAML to a CUE expression.
//
// If src != nil, Extract parses the source from src and the path is for
// position information. The type of the argument for the src parameter must be
// string, []byte, or io.Reader. If src == nil, ParseFile parses the file
// specified by filename.
func Extract(path string, src interface{}) (ast.Expr, error) {
	expr, err := extract(path, src)
	if err != nil {
		return nil, err
	}
	patchExpr(expr)
	return expr, nil
}

// Decode converts JSON file to a CUE value.
//
// If src != nil, Extract parses the source from src and the path is for
// position information. The type of the argument for the src parameter must be
// string, []byte, or io.Reader. If src == nil, ParseFile parses the file
// specified by filename.
func Decode(r *cue.Runtime, path string, src interface{}) (*cue.Instance, error) {
	expr, err := extract(path, src)
	if err != nil {
		return nil, err
	}
	return r.CompileExpr(expr)
}

func extract(path string, src interface{}) (ast.Expr, error) {
	b, err := source.Read(path, src)
	if err != nil {
		return nil, err
	}
	expr, err := parser.ParseExpr(path, b)
	if err != nil || !gojson.Valid(b) {
		// Get JSON-specific error, but
		p := token.NoPos
		if pos := errors.Positions(err); len(pos) > 0 {
			p = pos[0]
		}
		var x interface{}
		err := gojson.Unmarshal(b, &x)
		return nil, errors.Wrapf(err, p, "invalid JSON for file %q", path)
	}
	return expr, nil
}

// NewDecoder configures a JSON decoder. The path is used to associate position
// information with each node. The runtime may be nil if the decoder
// is only used to extract to CUE ast objects.
func NewDecoder(r *cue.Runtime, path string, src io.Reader) *Decoder {
	return &Decoder{
		r:      r,
		path:   path,
		dec:    gojson.NewDecoder(src),
		offset: 1,
	}
}

// A Decoder converts JSON values to CUE.
type Decoder struct {
	r      *cue.Runtime
	path   string
	dec    *gojson.Decoder
	offset int
}

// Extract converts the current JSON value to a CUE ast. It returns io.EOF
// if the input has been exhausted.
func (d *Decoder) Extract() (ast.Expr, error) {
	expr, err := d.extract()
	if err != nil {
		return expr, err
	}
	patchExpr(expr)
	return expr, nil
}

func (d *Decoder) extract() (ast.Expr, error) {
	var raw gojson.RawMessage
	err := d.dec.Decode(&raw)
	if err == io.EOF {
		return nil, err
	}
	offset := d.offset
	d.offset += len(raw)
	if err != nil {
		pos := token.NewFile(d.path, offset, len(raw)).Pos(0, 0)
		return nil, errors.Wrapf(err, pos, "invalid JSON for file %q", d.path)
	}
	expr, err := parser.ParseExpr(d.path, []byte(raw), parser.FileOffset(offset))
	if err != nil {
		return nil, err
	}
	return expr, nil
}

// Decode converts the current JSON value to a CUE instance. It returns io.EOF
// if the input has been exhausted.
func (d *Decoder) Decode() (*cue.Instance, error) {
	expr, err := d.Extract()
	if err != nil {
		return nil, err
	}
	return d.r.CompileExpr(expr)
}

// patchExpr simplifies the AST parsed from JSON.
// TODO: some of the modifications are already done in format, but are
// a package deal of a more aggressive simplify. Other pieces of modification
// should probably be moved to format.
func patchExpr(n ast.Node) {
	type info struct {
		reflow bool
	}
	stack := []info{{true}}

	afterFn := func(n ast.Node) {
		switch n.(type) {
		case *ast.ListLit, *ast.StructLit:
			stack = stack[:len(stack)-1]
		}
	}

	var beforeFn func(n ast.Node) bool

	beforeFn = func(n ast.Node) bool {
		isLarge := n.End().Offset()-n.Pos().Offset() > 50
		descent := true

		switch x := n.(type) {
		case *ast.ListLit:
			reflow := true
			if !isLarge {
				for _, e := range x.Elts {
					if hasSpaces(e) {
						reflow = false
						break
					}
				}
			}
			stack = append(stack, info{reflow})
			if reflow {
				x.Lbrack = x.Lbrack.WithRel(token.NoRelPos)
				x.Rbrack = x.Rbrack.WithRel(token.NoRelPos)
			}
			return true

		case *ast.StructLit:
			reflow := true
			if !isLarge {
				for _, e := range x.Elts {
					if f, ok := e.(*ast.Field); !ok || hasSpaces(f) || hasSpaces(f.Value) {
						reflow = false
						break
					}
				}
			}
			stack = append(stack, info{reflow})
			if reflow {
				x.Lbrace = x.Lbrace.WithRel(token.NoRelPos)
				x.Rbrace = x.Rbrace.WithRel(token.NoRelPos)
			}
			return true

		case *ast.Field:
			// label is always a string for JSON.
			switch {
			case true:
				s, ok := x.Label.(*ast.BasicLit)
				if !ok || s.Kind != token.STRING {
					break // should not happen: implies invalid JSON
				}

				u, err := literal.Unquote(s.Value)
				if err != nil {
					break // should not happen: implies invalid JSON
				}

				if q, err := ast.QuoteIdent(u); err != nil || q != u {
					break
				}

				x.Label = ast.NewIdent(u)
				astutil.CopyMeta(x.Label, s)
			}
			ast.Walk(x.Value, beforeFn, afterFn)
			descent = false

		case *ast.BasicLit:
			if x.Kind == token.STRING && len(x.Value) > 10 {
				s, err := literal.Unquote(x.Value)
				if err != nil {
					break // should not happen: implies invalid JSON
				}

				lines := strings.Split(s, "\n")
				if len(lines) == 1 {
					break
				}
				x.Value = quoteMulti(lines, len(stack))
			}
		}

		if stack[len(stack)-1].reflow {
			ast.SetRelPos(n, token.NoRelPos)
		}
		return descent
	}

	ast.Walk(n, beforeFn, afterFn)
}

func hasSpaces(n ast.Node) bool {
	return n.Pos().RelPos() > token.NoSpace
}

func quoteMulti(a []string, indent int) string {
	b := bytes.Buffer{}
	prefix := "\n" + strings.Repeat("\t", indent)
	b.WriteString(`"""`)
	for _, s := range a {
		b.WriteString(prefix)
		q := strconv.Quote(s)
		b.WriteString(q[1 : len(q)-1])
	}
	b.WriteString(prefix)
	b.WriteString(`"""`)
	return b.String()
}
