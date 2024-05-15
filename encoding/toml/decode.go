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
	"strconv"

	toml "github.com/pelletier/go-toml/v2/unstable"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
)

// TODO(mvdan): filename, schema, and decode options

// NewDecoder creates a decoder from a stream of TOML input.
func NewDecoder(r io.Reader) *Decoder {
	// Note that we don't consume the reader here,
	// as there's no need, and we can't return an error either.
	return &Decoder{r: r, seenKeys: make(map[string]bool)}
}

// Decoder implements the decoding state.
//
// Note that TOML files and streams never decode multiple CUE nodes;
// subsequent calls to [Decoder.Decode] may return [io.EOF].
type Decoder struct {
	r io.Reader

	decoded bool // whether [Decoder.Decoded] has been called already
	parser  toml.Parser

	// seenKeys tracks which dot-separated rooted keys we have already decoded,
	// as duplicate keys in TOML are not allowed.
	// The string elements in between the dots may be quoted to avoid ambiguity.
	seenKeys map[string]bool

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
	// Note that if the input is empty the result will be the same
	// as for an empty table: an empty struct.
	// The TOML spec and other decoders also work this way.
	file := &ast.File{}
	for d.parser.NextExpression() {
		if err := d.nextRootNode(d.parser.Expression()); err != nil {
			return nil, err
		}
	}
	if err := d.parser.Error(); err != nil {
		return nil, err
	}
	for _, field := range d.currentFields {
		file.Decls = append(file.Decls, field)
	}
	d.currentFields = d.currentFields[:0]
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
	//   foo.title = "Foo"
	//   foo.bar.baz = "value"
	//
	// We decode them as "inline" structs in CUE, which keeps the original shape:
	//
	//   foo: title: "Foo"
	//   foo: bar: baz: "value"
	//
	// An alternative would be to join struct literals, which avoids some repetition,
	// but also introduces extra lines and may break some comment positions:
	//
	//   foo: {
	//       title: "Foo"
	//       bar: baz: "value"
	//   }
	case toml.KeyValue:
		field, err := d.decodeField("", tnode)
		if err != nil {
			return err
		}
		d.currentFields = append(d.currentFields, field)
	// TODO(mvdan): tables
	// TODO(mvdan): array tables
	default:
		return fmt.Errorf("encoding/toml.Decoder.nextRootNode: unknown %s %#v\n", tnode.Kind, tnode)
	}
	return nil
}

func quoteLabelIfNeeded(name string) string {
	if ast.IsValidIdent(name) {
		return name
	}
	return literal.Label.Quote(name)
}

// nextRootNode is called for every top-level expression from the TOML parser.
func (d *Decoder) decodeExpr(rootKey string, tnode *toml.Node) (ast.Expr, error) {
	// TODO(mvdan): we currently assume that TOML basic literals (string, int, float)
	// are also valid CUE literals; we should double check this, perhaps via fuzzing.
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
			// A path into an array element is like "arr.3",
			// which looks very similar to a table's "tbl.key",
			// particularly since a table key can be any string.
			// However, we just need these keys to detect duplicates,
			// and a path cannot be both an array and table, so it's OK.
			rootKey := rootKey + "." + strconv.Itoa(len(list.Elts))
			elem, err := d.decodeExpr(rootKey, elems.Node())
			if err != nil {
				return nil, err
			}
			list.Elts = append(list.Elts, elem)
		}
		return list, nil
	case toml.InlineTable:
		strct := &ast.StructLit{
			// We want a single-line struct, just like TOML's inline tables are on a single line.
			Lbrace: token.NoPos.WithRel(token.Blank),
			Rbrace: token.NoPos.WithRel(token.Blank),
		}
		elems := tnode.Children()
		for elems.Next() {
			field, err := d.decodeField(rootKey, elems.Node())
			if err != nil {
				return nil, err
			}
			strct.Elts = append(strct.Elts, field)
		}
		return strct, nil
	// TODO(mvdan): dates and times
	default:
		return nil, fmt.Errorf("encoding/toml.Decoder.decodeExpr: unknown %s %#v\n", tnode.Kind, tnode)
	}
}

func (d *Decoder) decodeField(rootKey string, tnode *toml.Node) (*ast.Field, error) {
	keys := tnode.Key()
	curName := string(keys.Node().Data)

	relPos := token.Newline
	if rootKey != "" {
		rootKey += "."
		relPos = token.Blank
	}
	rootKey += quoteLabelIfNeeded(curName)
	curField := &ast.Field{
		Label: &ast.Ident{
			NamePos: token.NoPos.WithRel(relPos),
			Name:    curName,
		},
	}

	topField := curField
	keys.Next() // TODO(mvdan): for some reason the first Next call doesn't count?
	for keys.Next() {
		nextName := string(keys.Node().Data)
		nextField := &ast.Field{
			Label: &ast.Ident{
				NamePos: token.NoPos.WithRel(token.Blank),
				Name:    nextName,
			},
		}
		curField.Value = &ast.StructLit{Elts: []ast.Decl{nextField}}
		curField = nextField
		// TODO(mvdan): use an append-like API once we have benchmarks
		rootKey += "." + quoteLabelIfNeeded(nextName)
	}
	if d.seenKeys[rootKey] {
		return nil, fmt.Errorf("duplicate key: %s", rootKey)
	}
	d.seenKeys[rootKey] = true
	value, err := d.decodeExpr(rootKey, tnode.Value())
	if err != nil {
		return nil, err
	}
	curField.Value = value
	return topField, nil
}
