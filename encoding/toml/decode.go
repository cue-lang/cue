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
	"strings"

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

	// topFile is the top-level CUE file we are decoding into.
	topFile *ast.File

	// openTableArrays keeps track of all the declared table arrays so that
	// later headers can append a new table array element, or add a field
	// to the last element in a table array.
	//
	// TODO(mvdan): an unsorted slice means we do two linear searches per header key.
	// For N distinct `[[keys]]`, this means a decoding runtime of O(2*N*N).
	// Consider either sorting this array so we can do a binary search for O(N*log2(N)),
	// or perhaps a tree, although for a nesting level D, that could cause O(N*D),
	// and a tree would use more slices and so more allocations.
	openTableArrays []openTableArray

	// currentTableKey is the rooted key for the current table where the following
	// TOML `key = value` lines will be inserted.
	currentTableKey string

	// currentTable is the CUE struct literal for currentTableKey.
	// It is nil before the first [header] or [[header]],
	// in which case any key-values are inserted in topFile.
	currentTable *ast.StructLit
}

type openTableArray struct {
	key   string
	level int // the level of nesting, e.g. 2 for key="foo.bar"
	list  *ast.ListLit
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
	d.topFile = &ast.File{}
	for d.parser.NextExpression() {
		if err := d.nextRootNode(d.parser.Expression()); err != nil {
			return nil, err
		}
	}
	if err := d.parser.Error(); err != nil {
		return nil, err
	}
	return d.topFile, nil
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
		// Top-level fields begin a new line.
		field, err := d.decodeField(d.currentTableKey, tnode, token.Newline)
		if err != nil {
			return err
		}
		if d.currentTable != nil {
			d.currentTable.Elts = append(d.currentTable.Elts, field)
		} else {
			d.topFile.Decls = append(d.topFile.Decls, field)
		}
	case toml.Table:
		// Tables always begin a new line.
		key, keyElems := decodeKey("", tnode.Key())
		// Table keys must be unique.
		if d.seenKeys[key] {
			return fmt.Errorf("duplicate key: %s", key)
		}
		d.seenKeys[key] = true

		// We want a multi-line struct with curly braces,
		// just like TOML's tables are on multiple lines.
		d.currentTable = &ast.StructLit{
			Lbrace: token.NoPos.WithRel(token.Blank),
			Rbrace: token.NoPos.WithRel(token.Newline),
		}
		array, ok := d.arrayReference(key)
		if ok { // [last_array.new_table]
			subKeyElems := keyElems[array.level:]
			if len(subKeyElems) == 0 {
				// XXX: error; redeclaring [[last_array]] as [last_array]
			}
			topField, leafField := inlineFields(subKeyElems, token.Newline)
			// XXX: remove the need for type casts
			lastArrayElem := array.list.Elts[len(array.list.Elts)-1].(*ast.StructLit)
			lastArrayElem.Elts = append(lastArrayElem.Elts, topField)
			d.currentTableKey = key
			leafField.Value = d.currentTable
		} else { // [new_table]
			topField, leafField := inlineFields(keyElems, token.Newline)
			d.topFile.Decls = append(d.topFile.Decls, topField)
			d.currentTableKey = key
			leafField.Value = d.currentTable
		}

	case toml.ArrayTable:
		// Table array elements always begin a new line.
		key, keyElems := decodeKey("", tnode.Key())
		// XXX: error if redeclaring [key] as [[key]]

		// Table array keys do not need to be unique; when they repeat the current key,
		// they append to an existing array.
		switch array, ok := d.arrayReference(key); {
		case !ok: // [[new_array]]
			// We want a multi-line list with square braces,
			// since TOML's table arrays are on multiple lines.
			list := &ast.ListLit{
				Lbrack: token.NoPos.WithRel(token.Blank),
				Rbrack: token.NoPos.WithRel(token.Newline),
			}
			d.openTableArrays = append(d.openTableArrays, openTableArray{
				key:   key,
				level: len(keyElems),
				list:  list,
			})

			topField, leafField := inlineFields(keyElems, token.Newline)
			d.topFile.Decls = append(d.topFile.Decls, topField)
			leafField.Value = list

			// Each struct inside a table array sits on separate lines.
			d.currentTable = &ast.StructLit{
				Lbrace: token.NoPos.WithRel(token.Newline),
				Rbrace: token.NoPos.WithRel(token.Newline),
			}
			d.currentTableKey = key // XXX: missing index?
			list.Elts = append(list.Elts, d.currentTable)

		case len(keyElems) > array.level: // [[last_array.new_array]]
			// We want a multi-line list with square braces,
			// since TOML's table arrays are on multiple lines.
			list := &ast.ListLit{
				Lbrack: token.NoPos.WithRel(token.Blank),
				Rbrack: token.NoPos.WithRel(token.Newline),
			}
			d.openTableArrays = append(d.openTableArrays, openTableArray{
				key:   key,
				level: len(keyElems),
				list:  list,
			})

			subKeyElems := keyElems[array.level:]
			topField, leafField := inlineFields(subKeyElems, token.Newline)
			// XXX: remove the need for type casts
			lastArrayElem := array.list.Elts[len(array.list.Elts)-1].(*ast.StructLit)
			lastArrayElem.Elts = append(lastArrayElem.Elts, topField)
			leafField.Value = list

			// Each struct inside a table array sits on separate lines.
			d.currentTable = &ast.StructLit{
				Lbrace: token.NoPos.WithRel(token.Newline),
				Rbrace: token.NoPos.WithRel(token.Newline),
			}
			d.currentTableKey = key // XXX: missing index?
			list.Elts = append(list.Elts, d.currentTable)
		default: // [[last_array]]
			// Each struct inside a table array sits on separate lines.
			d.currentTable = &ast.StructLit{
				Lbrace: token.NoPos.WithRel(token.Newline),
				Rbrace: token.NoPos.WithRel(token.Newline),
			}
			d.currentTableKey = key // XXX: missing index?
			array.list.Elts = append(array.list.Elts, d.currentTable)
		}

	default:
		return fmt.Errorf("encoding/toml.Decoder.nextRootNode: unknown %s %#v", tnode.Kind, tnode)
	}
	return nil
}

func (d *Decoder) decodeField(key string, tnode *toml.Node, relPos token.RelPos) (*ast.Field, error) {
	key, keyElems := decodeKey(key, tnode.Key())
	topField, leafField := inlineFields(keyElems, relPos)
	// Table inner keys must be unique.
	if d.seenKeys[key] {
		return nil, fmt.Errorf("duplicate key: %s", key)
	}
	d.seenKeys[key] = true
	value, err := d.decodeExpr(key, tnode.Value())
	if err != nil {
		return nil, err
	}
	leafField.Value = value
	return topField, nil
}

func (d *Decoder) arrayReference(key string) (openTableArray, bool) {
	// TODO(mvdan): see the performance TODO on [Decoder.openTableArrays].

	// Prefer an exact match over a relative prefix match.
	for _, arr := range d.openTableArrays {
		if arr.key == key {
			return arr, true
		}
	}
	// The longest relative key match wins.
	maxLevel := 0
	maxLevelArr := openTableArray{}
	for _, arr := range d.openTableArrays {
		if relativeKey(arr.key, key) != "" && arr.level > maxLevel {
			maxLevel = arr.level
			maxLevelArr = arr
		}
	}
	if maxLevel > 0 {
		return maxLevelArr, true
	}
	return openTableArray{}, false
}

func relativeKey(baseKey, targetKey string) string {
	if baseKey == "" {
		return ""
	}
	relKey, ok := strings.CutPrefix(targetKey, baseKey+".")
	if !ok {
		return ""
	}
	return relKey
}

func decodeKey(key string, iter toml.Iterator) (string, []string) {
	var elems []string
	for i := 0; iter.Next(); i++ {
		name := string(iter.Node().Data)
		// TODO(mvdan): use an append-like API once we have benchmarks
		if len(key) > 0 {
			key += "."
		}
		key += quoteLabelIfNeeded(name)
		elems = append(elems, name)
	}
	return key, elems
}

func inlineFields(names []string, relPos token.RelPos) (top, leaf *ast.Field) {
	curField := &ast.Field{
		Label: &ast.Ident{
			NamePos: token.NoPos.WithRel(relPos),
			Name:    names[0],
		},
	}

	topField := curField
	for _, elem := range names[1:] {
		nextField := &ast.Field{
			Label: &ast.Ident{
				NamePos: token.NoPos.WithRel(token.Blank),
				Name:    elem,
			},
		}
		curField.Value = &ast.StructLit{Elts: []ast.Decl{nextField}}
		curField = nextField
	}
	return topField, curField
}

func quoteLabelIfNeeded(name string) string {
	if ast.IsValidIdent(name) {
		return name
	}
	return literal.Label.Quote(name)
}

func (d *Decoder) decodeExpr(key string, tnode *toml.Node) (ast.Expr, error) {
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
			key := key + "." + strconv.Itoa(len(list.Elts))
			elem, err := d.decodeExpr(key, elems.Node())
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
			// Inline table fields are on the same line.
			field, err := d.decodeField(key, elems.Node(), token.Blank)
			if err != nil {
				return nil, err
			}
			strct.Elts = append(strct.Elts, field)
		}
		return strct, nil
	// TODO(mvdan): dates and times
	default:
		return nil, fmt.Errorf("encoding/toml.Decoder.decodeExpr: unknown %s %#v", tnode.Kind, tnode)
	}
}
