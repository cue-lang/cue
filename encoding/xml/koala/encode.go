// Copyright 2025 The CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package koala

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"cuelang.org/go/cue"
)

// Encoder writes CUE values as XML using the koala encoding.
type Encoder struct {
	w       io.Writer
	indents []string // cached indentation strings, grown on demand
}

// NewEncoder creates an encoder that writes XML to w.
func NewEncoder(w io.Writer) *Encoder {
	return &Encoder{w: w}
}

// indent returns the indentation string for the given depth,
// caching computed strings so repeated calls at the same depth
// do not allocate.
func (enc *Encoder) indent(depth int) string {
	for len(enc.indents) <= depth {
		enc.indents = append(enc.indents, strings.Repeat("\t", len(enc.indents)))
	}
	return enc.indents[depth]
}

// Encode writes v as a koala-encoded XML document.
// The value must be a struct with exactly one field, whose label
// becomes the root XML element name.
func (enc *Encoder) Encode(v cue.Value) error {
	// Emit XML declaration.
	if _, err := io.WriteString(enc.w, "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n"); err != nil {
		return err
	}

	// Resolve default markers so that *X | Y becomes X.
	v, _ = v.Default()

	// The top-level value must be a struct with exactly one field (the root element).
	if v.Kind() != cue.StructKind {
		return fmt.Errorf("koala: top-level value must be a struct, got %v", v.Kind())
	}
	iter, err := v.Fields()
	if err != nil {
		return fmt.Errorf("koala: iterating top-level fields: %w", err)
	}
	if !iter.Next() {
		return fmt.Errorf("koala: top-level struct has no fields (need exactly one root element)")
	}
	rootName := iter.Selector().Unquoted()
	rootVal := iter.Value()
	if iter.Next() {
		return fmt.Errorf("koala: top-level struct has multiple fields (XML requires exactly one root element)")
	}

	return enc.encodeValue(rootName, rootVal, 0)
}

// encodeValue dispatches encoding based on the CUE value kind.
func (enc *Encoder) encodeValue(name string, v cue.Value, depth int) error {
	v, _ = v.Default()
	switch v.Kind() {
	case cue.StructKind:
		return enc.encodeStruct(name, v, depth)
	case cue.ListKind:
		if isContainer(v) {
			return enc.encodeContainer(name, v, depth)
		}
		return enc.encodeList(name, v, depth)
	case cue.StringKind, cue.IntKind, cue.FloatKind, cue.BoolKind:
		return enc.encodeScalar(name, v, depth)
	default:
		return fmt.Errorf("koala: unsupported value kind %v for element %q", v.Kind(), name)
	}
}

// encodeStruct writes an XML element from a koala-convention CUE struct.
// Fields prefixed with "$" become XML attributes, "$$" becomes text content,
// and all other fields become child elements.
func (enc *Encoder) encodeStruct(name string, v cue.Value, depth int) error {
	type field struct {
		name  string
		value cue.Value
	}

	var attrs []field
	var children []field
	var textContent *string

	iter, err := v.Fields()
	if err != nil {
		return err
	}
	for iter.Next() {
		label := iter.Selector().Unquoted()
		val := iter.Value()

		if label == contentAttribute {
			s, err := valueToStr(val)
			if err != nil {
				return fmt.Errorf("koala: converting text content to string: %w", err)
			}
			textContent = &s
		} else if strings.HasPrefix(label, attributeSymbol) {
			attrs = append(attrs, field{name: label, value: val})
		} else {
			children = append(children, field{name: label, value: val})
		}
	}

	w := enc.w

	// Build start tag with attributes.
	if _, err := io.WriteString(w, enc.indent(depth)); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "<"); err != nil {
		return err
	}
	if _, err := io.WriteString(w, name); err != nil {
		return err
	}
	for _, a := range attrs {
		attrName := a.name[len(attributeSymbol):]
		attrVal, err := valueToStr(a.value)
		if err != nil {
			return fmt.Errorf("koala: converting attribute %q to string: %w", attrName, err)
		}
		if _, err := io.WriteString(w, " "); err != nil {
			return err
		}
		if _, err := io.WriteString(w, attrName); err != nil {
			return err
		}
		if _, err := io.WriteString(w, "=\""); err != nil {
			return err
		}
		if _, err := io.WriteString(w, escapeAttr(attrVal)); err != nil {
			return err
		}
		if _, err := io.WriteString(w, "\""); err != nil {
			return err
		}
	}

	// Self-closing tag: no text content and no children.
	if textContent == nil && len(children) == 0 {
		_, err := io.WriteString(w, "/>\n")
		return err
	}

	// Mixed content (text content and child elements) is not representable
	// in XML without mixed-content mode, which koala does not support.
	// Mirror the decoder's rejection of this case.
	if textContent != nil && len(children) > 0 {
		return fmt.Errorf("koala: element %q has both text content ($$) and child elements", name)
	}

	// Inline text content (no child elements).
	if textContent != nil {
		if _, err := io.WriteString(w, ">"); err != nil {
			return err
		}
		if _, err := io.WriteString(w, escapeText(*textContent)); err != nil {
			return err
		}
		if _, err := io.WriteString(w, "</"); err != nil {
			return err
		}
		if _, err := io.WriteString(w, name); err != nil {
			return err
		}
		_, err := io.WriteString(w, ">\n")
		return err
	}

	// Element with children.
	if _, err := io.WriteString(w, ">\n"); err != nil {
		return err
	}
	for _, child := range children {
		if err := enc.encodeValue(child.name, child.value, depth+1); err != nil {
			return err
		}
	}
	if _, err := io.WriteString(w, enc.indent(depth)); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "</"); err != nil {
		return err
	}
	if _, err := io.WriteString(w, name); err != nil {
		return err
	}
	_, err = io.WriteString(w, ">\n")
	return err
}

// encodeList writes repeated XML elements with the same tag name,
// one per list item.
func (enc *Encoder) encodeList(name string, v cue.Value, depth int) error {
	iter, err := v.List()
	if err != nil {
		return err
	}
	for iter.Next() {
		if err := enc.encodeValue(name, iter.Value(), depth); err != nil {
			return err
		}
	}
	return nil
}

// encodeContainer writes a single wrapper element whose children are the
// unwrapped fields of each list item. Each item must be a struct.
// This is triggered by the @koala(container) attribute on a list field.
//
// Given chain: [{a: {$$: "1"}}, {b: {$$: "2"}}] @koala(container), it produces:
//
//	<chain>
//		<a>1</a>
//		<b>2</b>
//	</chain>
func (enc *Encoder) encodeContainer(name string, v cue.Value, depth int) error {
	iter, err := v.List()
	if err != nil {
		return err
	}

	// Collect items; empty list emits nothing.
	var items []cue.Value
	for iter.Next() {
		items = append(items, iter.Value())
	}
	if len(items) == 0 {
		return nil
	}

	w := enc.w

	// Open container element.
	if _, err := io.WriteString(w, enc.indent(depth)); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "<"); err != nil {
		return err
	}
	if _, err := io.WriteString(w, name); err != nil {
		return err
	}
	if _, err := io.WriteString(w, ">\n"); err != nil {
		return err
	}

	// Emit each struct item's fields directly inside the container.
	for _, item := range items {
		item, _ = item.Default()
		if item.Kind() != cue.StructKind {
			return fmt.Errorf("koala: container element %q: list items must be structs, got %v", name, item.Kind())
		}
		fields, err := item.Fields()
		if err != nil {
			return err
		}
		for fields.Next() {
			if err := enc.encodeValue(fields.Selector().Unquoted(), fields.Value(), depth+1); err != nil {
				return err
			}
		}
	}

	// Close container element.
	if _, err := io.WriteString(w, enc.indent(depth)); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "</"); err != nil {
		return err
	}
	if _, err := io.WriteString(w, name); err != nil {
		return err
	}
	_, err = io.WriteString(w, ">\n")
	return err
}

// isContainer reports whether v carries a @koala(container) field attribute.
func isContainer(v cue.Value) bool {
	a := v.Attribute("koala")
	if a.Err() != nil {
		return false
	}
	s, err := a.String(0)
	return err == nil && s == "container"
}

// encodeScalar writes a leaf XML element containing a stringified scalar value.
func (enc *Encoder) encodeScalar(name string, v cue.Value, depth int) error {
	s, err := valueToStr(v)
	if err != nil {
		return err
	}
	w := enc.w
	if _, err := io.WriteString(w, enc.indent(depth)); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "<"); err != nil {
		return err
	}
	if _, err := io.WriteString(w, name); err != nil {
		return err
	}
	if _, err := io.WriteString(w, ">"); err != nil {
		return err
	}
	if _, err := io.WriteString(w, escapeText(s)); err != nil {
		return err
	}
	if _, err := io.WriteString(w, "</"); err != nil {
		return err
	}
	if _, err := io.WriteString(w, name); err != nil {
		return err
	}
	_, err = io.WriteString(w, ">\n")
	return err
}

// valueToStr converts a CUE scalar value to its string representation for XML output.
func valueToStr(v cue.Value) (string, error) {
	v, _ = v.Default()
	switch v.Kind() {
	case cue.StringKind:
		return v.String()
	case cue.BoolKind:
		b, err := v.Bool()
		if err != nil {
			return "", err
		}
		return strconv.FormatBool(b), nil
	case cue.IntKind:
		i, err := v.Int64()
		if err != nil {
			return "", err
		}
		return strconv.FormatInt(i, 10), nil
	case cue.FloatKind:
		d, _ := v.Decimal()
		return d.String(), nil
	default:
		return "", fmt.Errorf("koala: cannot convert %v to string", v.Kind())
	}
}

// escapeText escapes special XML characters in text content.
func escapeText(s string) string {
	if !strings.ContainsAny(s, "&<>") {
		return s
	}
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	return s
}

// escapeAttr escapes special XML characters in attribute values.
func escapeAttr(s string) string {
	if !strings.ContainsAny(s, "&<>\"") {
		return s
	}
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, "\"", "&quot;")
	return s
}
