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

// Package schema converts XML to and from CUE using a CUE schema to guide
// the mapping.
//
// Fields in the CUE schema use @xml attributes to define how XML elements,
// attributes, and text content map to CUE fields:
//
//   - @xml(tag=<name>) matches an XML element with the given tag name (default: CUE field name)
//   - @xml(attr=<name>) maps to an XML attribute on the parent element
//   - @xml(body) maps to the text content of the parent element
//   - @xml(ns=<prefix>) specifies a namespace prefix for matching
//
// Lists are inferred from the CUE type: if a field is [...#Foo], repeated XML
// elements with the matching tag are collected into the list.
//
// WARNING: THIS PACKAGE IS EXPERIMENTAL.
// ITS API MAY CHANGE AT ANY TIME.
package schema

import (
	"encoding/xml"
	"fmt"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/token"
)

// NewDecoder returns a new [Decoder].
func NewDecoder(option ...Option) *Decoder {
	d := &Decoder{}
	for _, o := range option {
		o(&d.opts)
	}
	return d
}

// A Decoder caches conversions of [cue.Value] between calls to its methods.
type Decoder struct {
	opts         options
	schemaParser schemaParser
}

type decoder struct {
	*Decoder
	errs errors.Error
	file *token.File
}

// Parse parses the given XML bytes and converts them to a CUE expression,
// using schema as a guideline for the conversion. Fields in the schema
// use @xml attributes to control how XML elements, attributes, and text
// content map to CUE fields.
//
// Fields in the XML that have no corresponding field in the schema are ignored.
func (d *Decoder) Parse(schema cue.Value, filename string, b []byte) (ast.Expr, error) {
	dec := decoder{Decoder: d}

	f := token.NewFile(filename, -1, len(b))
	f.SetLinesForContent(b)
	dec.file = f

	m := d.schemaParser.parseSchema(schema)
	if d.schemaParser.errs != nil {
		err := d.schemaParser.errs
		d.schemaParser.errs = nil
		return nil, err
	}

	xdec := xml.NewDecoder(strings.NewReader(string(b)))

	// Find the root element.
	for {
		tok, err := xdec.Token()
		if err != nil {
			return nil, errors.Newf(token.NoPos, "xml: %v", err)
		}
		if start, ok := tok.(xml.StartElement); ok {
			result := dec.decodeElement(m, start, xdec)
			if dec.errs != nil {
				return nil, dec.errs
			}
			return result, nil
		}
	}
}

func (d *decoder) addErr(err error) {
	d.errs = errors.Append(d.errs, errors.Promote(err, "xml"))
}

// decodeElement decodes an XML element into a CUE struct expression.
func (d *decoder) decodeElement(m *mapping, start xml.StartElement, xdec *xml.Decoder) ast.Expr {
	if m == nil {
		// No schema for this element; skip entirely.
		xdec.Skip()
		return &ast.StructLit{}
	}

	st := &ast.StructLit{}
	var listMap map[string]*ast.ListLit

	// Process XML attributes.
	for _, attr := range start.Attr {
		name := attr.Name.Local
		fi, ok := d.matchAttr(m, name, attr.Name.Space)
		if !ok {
			continue
		}
		st.Elts = append(st.Elts, &ast.Field{
			Label: ast.NewStringLabel(fi.cueName),
			Value: d.decodeLeaf(fi, attr.Value),
		})
	}

	// Process child elements and text content.
	var chardata strings.Builder
	for {
		tok, err := xdec.Token()
		if err != nil {
			d.addErr(fmt.Errorf("xml: %v", err))
			return st
		}

		switch t := tok.(type) {
		case xml.StartElement:
			fi, ok := d.matchChild(m, t.Name.Local, t.Name.Space)
			if !ok {
				xdec.Skip()
				continue
			}

			if fi.isList {
				if listMap == nil {
					listMap = make(map[string]*ast.ListLit)
				}
				list := listMap[fi.cueName]
				if list == nil {
					list = &ast.ListLit{}
					listMap[fi.cueName] = list
					st.Elts = append(st.Elts, &ast.Field{
						Label: ast.NewStringLabel(fi.cueName),
						Value: list,
					})
				}
				var elem ast.Expr
				if fi.msg != nil {
					elem = d.decodeElement(fi.msg, t, xdec)
				} else {
					// List of scalars.
					text := d.readText(xdec)
					elemFI := &fieldInfo{value: fi.value.LookupPath(cue.MakePath(cue.AnyIndex))}
					elem = d.decodeLeaf(elemFI, text)
				}
				if elem != nil {
					list.Elts = append(list.Elts, elem)
				}
				continue
			}

			var val ast.Expr
			if fi.msg != nil {
				val = d.decodeElement(fi.msg, t, xdec)
			} else {
				text := d.readText(xdec)
				val = d.decodeLeaf(fi, text)
			}
			if val != nil {
				st.Elts = append(st.Elts, &ast.Field{
					Label: ast.NewStringLabel(fi.cueName),
					Value: val,
				})
			}

		case xml.CharData:
			chardata.Write(t)

		case xml.EndElement:
			// Handle body field.
			if m.body != nil {
				text := strings.TrimSpace(chardata.String())
				if text != "" {
					st.Elts = append(st.Elts, &ast.Field{
						Label: ast.NewStringLabel(m.body.cueName),
						Value: d.decodeLeaf(m.body, text),
					})
				}
			}
			return st
		}
	}
}

// readText reads all character data inside an element until EndElement,
// consuming the end element token.
func (d *decoder) readText(xdec *xml.Decoder) string {
	var buf strings.Builder
	for {
		tok, err := xdec.Token()
		if err != nil {
			d.addErr(fmt.Errorf("xml: %v", err))
			return buf.String()
		}
		switch t := tok.(type) {
		case xml.CharData:
			buf.Write(t)
		case xml.EndElement:
			return buf.String()
		}
	}
}

// matchAttr looks up an XML attribute name in the mapping.
func (d *decoder) matchAttr(m *mapping, name, space string) (*fieldInfo, bool) {
	fi, ok := m.attrs[name]
	if !ok {
		return nil, false
	}
	if fi.ns != "" {
		uri := d.opts.namespaces[fi.ns]
		if uri != space {
			return nil, false
		}
	}
	return fi, true
}

// matchChild looks up an XML child element tag in the mapping.
func (d *decoder) matchChild(m *mapping, name, space string) (*fieldInfo, bool) {
	fi, ok := m.children[name]
	if !ok {
		return nil, false
	}
	if fi.ns != "" {
		uri := d.opts.namespaces[fi.ns]
		if uri != space {
			return nil, false
		}
	}
	return fi, true
}

// decodeLeaf converts a string text value to a CUE literal expression,
// based on the field's IncompleteKind.
func (d *decoder) decodeLeaf(fi *fieldInfo, text string) ast.Expr {
	kind := fi.value.IncompleteKind()
	switch kind {
	case cue.StringKind:
		return &ast.BasicLit{Kind: token.STRING, Value: literal.String.Quote(text)}

	case cue.BoolKind:
		switch text {
		case "true":
			return ast.NewBool(true)
		case "false":
			return ast.NewBool(false)
		default:
			d.addErr(fmt.Errorf("xml: invalid bool value %q", text))
			return ast.NewBool(false)
		}

	case cue.IntKind:
		var info literal.NumInfo
		if err := literal.ParseNum(text, &info); err != nil {
			d.addErr(fmt.Errorf("xml: invalid integer %q", text))
			return &ast.BasicLit{Kind: token.INT, Value: text}
		}
		return &ast.BasicLit{Kind: token.INT, Value: info.String()}

	case cue.FloatKind:
		return &ast.BasicLit{Kind: token.FLOAT, Value: text}

	case cue.NumberKind:
		var info literal.NumInfo
		if err := literal.ParseNum(text, &info); err != nil {
			d.addErr(fmt.Errorf("xml: invalid number %q", text))
			return &ast.BasicLit{Kind: token.INT, Value: text}
		}
		if !info.IsInt() {
			return &ast.BasicLit{Kind: token.FLOAT, Value: text}
		}
		return &ast.BasicLit{Kind: token.INT, Value: info.String()}

	default:
		// Default to string for unknown types.
		return &ast.BasicLit{Kind: token.STRING, Value: literal.String.Quote(text)}
	}
}
