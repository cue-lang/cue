// Copyright 2024 The CUE Authors
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

// Package toml converts TOML to and from CUE.
//
// WARNING: THIS PACKAGE IS EXPERIMENTAL.
// ITS API MAY CHANGE AT ANY TIME.
package toml

import (
	"fmt"
	"io"

	toml "github.com/pelletier/go-toml/v2/unstable"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

// TODO(mvdan): filename, schema, and decode options

// NewDecoder creates a decoder from a stream of TOML input.
func NewDecoder(r io.Reader) *Decoder {
	// Note that we don't consume the reader here,
	// as there's no need, and we can't return an error either.
	return &Decoder{r: r}
}

// Decoder implements the decoding state.
//
// Note that TOML files and streams never decode multiple CUE nodes;
// subsequent calls to [Decoder.Decode] may return [io.EOF].
type Decoder struct {
	r io.Reader

	decoded bool // whether [Decoder.Decoded] has been called already
	parser  toml.Parser

	currentFields []*ast.Field
}

// TODO(mvdan): support decoding comments
// TODO(mvdan): support ast.Node positions
// TODO(mvdan): support error positions

// Decode parses the input stream as TOML and converts it to a CUE [*ast.File].
// Because TOML files only contain a single top-level expression,
// subsequent calls to this method may return [io.EOF].
func (d *Decoder) Decode() (ast.Node, error) {
	if d.decoded {
		return nil, io.EOF
	}
	d.decoded = true
	// TODO(mvdan): unfortunately go-toml does not support streaming as of v2.2.2.
	data, err := io.ReadAll(d.r)
	if err != nil {
		return nil, err
	}
	d.parser.Reset(data)
	file := &ast.File{}
	for d.parser.NextExpression() {
		if err := d.nextRootNode(d.parser.Expression()); err != nil {
			return nil, err
		}
	}
	for _, field := range d.currentFields {
		file.Decls = append(file.Decls, field)
	}
	d.currentFields = d.currentFields[:0]
	if len(file.Decls) == 0 {
		// Empty inputs are decoded as null, much like JSON or YAML.
		file.Decls = append(file.Decls, ast.NewNull())
	}
	return file, nil
}

// nextRootNode is called for every top-level expression from the TOML parser.
//
// This method does not return a syntax tree node directly,
// because some kinds of top-level expressions like comments and table headers
// require recording some state in the decoder to produce a node at a later time.
func (d *Decoder) nextRootNode(tnode *toml.Node) error {
	switch tnode.Kind {
	// Key-Values in TOML are in the form of:
	//
	//   foo.title   = "Foo"
	//   foo.bar.baz = "value"
	//
	// We can decode them as "inline" structs in CUE, which keeps the original shape:
	//
	//   foo: title:    "Foo"
	//   foo: bar: baz: "value"
	//
	// An alternative would be to join struct literals, which avoids some repetition,
	// but also introduces extra lines and may break some comment positions:
	//
	//   foo: {
	//       title:    "Foo"
	//       bar: baz: "value"
	//   }
	case toml.KeyValue:
		keys := tnode.Key()
		topField := &ast.Field{
			Label: &ast.Ident{
				NamePos: token.NoPos.WithRel(token.Newline),
				Name:    string(keys.Node().Data),
			},
		}
		ast.SetRelPos(topField.Label, token.Newline)
		keys.Next() // TODO(mvdan): for some reason the first Next call doesn't count?
		curField := topField
		for keys.Next() {
			nextField := &ast.Field{
				Label: &ast.Ident{
					NamePos: token.NoPos.WithRel(token.Blank),
					Name:    string(keys.Node().Data),
				},
			}
			ast.SetRelPos(nextField.Label, token.Blank)
			curField.Value = &ast.StructLit{Elts: []ast.Decl{nextField}}
			curField = nextField
		}
		value, err := d.decodeExpr(tnode.Value())
		if err != nil {
			return err
		}
		curField.Value = value
		d.currentFields = append(d.currentFields, topField)
	// TODO(mvdan): tables
	// TODO(mvdan): array tables
	default:
		return fmt.Errorf("encoding/toml.Decoder.nextRootNode: unknown %s %#v\n", tnode.Kind, tnode)
	}
	return nil
}

// nextRootNode is called for every top-level expression from the TOML parser.
func (d *Decoder) decodeExpr(tnode *toml.Node) (ast.Expr, error) {
	data := string(tnode.Data)
	switch tnode.Kind {
	case toml.String:
		return ast.NewString(data), nil
	case toml.Integer:
		return ast.NewLit(token.INT, data), nil
	case toml.Float:
		return ast.NewLit(token.FLOAT, data), nil
	case toml.Bool:
		return ast.NewBool(data == "true"), nil
	case toml.Array:
		list := &ast.ListLit{}
		elems := tnode.Children()
		for elems.Next() {
			elem, err := d.decodeExpr(elems.Node())
			if err != nil {
				return nil, err
			}
			list.Elts = append(list.Elts, elem)
		}
		return list, nil
	// TODO(mvdan): dates and times
	// TODO(mvdan): inline tables
	default:
		return nil, fmt.Errorf("encoding/toml.Decoder.decodeExpr: unknown %s %#v\n", tnode.Kind, tnode)
	}
	return nil, nil
}
