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

// Package koala converts XML to and from CUE, as described in the proposal for the [koala] encoding.
// This encoding is inspired by the [BadgerFish] convention for translating XML to JSON.
// It differs from this to better fit CUE syntax, (as "$" and "@" are special characters),
// and for improved readability, as described in the koala proposal.
//
// XML elements are modeled as CUE structs, their attributes are modeled as struct fields
// prefixed with "$", and their inner text content is modeled as a field named "$$".
//
// WARNING: THIS PACKAGE IS EXPERIMENTAL.
// ITS API MAY CHANGE AT ANY TIME.
//
// [koala]: https://cuelang.org/discussion/3776
// [BadgerFish]: http://www.sklar.com/badgerfish/
package koala

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"maps"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
)

// Decoder implements the decoding state.
type Decoder struct {
	reader    io.Reader
	fileName  string
	tokenFile *token.File

	decoderRan bool

	// current XML element being processed.
	currXmlElement *xmlElement

	// The top-level CUE struct.
	astRoot *ast.StructLit
	// CUE model of ancestors of current XML element being processed.
	ancestors []currFieldInfo
	// CUE model of current XML element being processed.
	currField currFieldInfo
	// Position of the pending text content of the current XML element, or [token.NoPos].
	currInnerTextPos token.Pos
	// The pending text content, which may span comments and CDATA sections.
	currInnerTextContent []byte
}

// currFieldInfo encapsulates details of the CUE field for the current XML element being processed.
type currFieldInfo struct {
	// CUE model of current XML element.
	field *ast.Field
	// Running map of the current field's children.
	currFieldChildren map[string]*ast.Field
}

// xmlElement models an XML Element hierarchy.
// It is used for tracking namespace prefixes.
type xmlElement struct {
	xmlName xml.Name
	// Position of the element's start tag.
	pos token.Pos
	// ns maps each namespace prefix in scope to its namespace.
	// It is shared with the parent element if this one declares no prefixes.
	ns       map[string]string
	parent   *xmlElement
	children []*xmlElement
}

// predefinedNamespaces maps the prefixes which are bound by definition
// to their namespaces.
var predefinedNamespaces = map[string]string{
	"xml":   "http://www.w3.org/XML/1998/namespace",
	"xmlns": "http://www.w3.org/2000/xmlns/",
}

// The prefix used to model the inner text content within an XML element.
const contentAttribute string = "$$"

// The prefix used to model each attribute of an XML element.
const attributeSymbol string = "$"

// NewDecoder creates a decoder from a stream of XML input.
func NewDecoder(fileName string, r io.Reader) *Decoder {
	return &Decoder{reader: r, fileName: fileName}
}

// Decode parses the input stream as XML and converts it to a CUE [ast.Expr].
// The input stream is taken from the [Decoder] and consumed.
//
// The result can be converted to a [cue.Value] via [cue.Context.BuildExpr].
func (dec *Decoder) Decode() (ast.Expr, error) {
	if dec.decoderRan {
		return nil, io.EOF
	}
	dec.decoderRan = true
	// TODO(mvdan): note that we read the whole input just for the sake of [token.NewFile];
	// either revamp that API so that it doesn't need the length upfront,
	// or lean into it and have internal/encoding pass the input size here.
	xmlText, err := io.ReadAll(dec.reader)
	if err != nil {
		return nil, err
	}
	reader := bytes.NewReader(xmlText)
	xmlDec := xml.NewDecoder(reader)

	// Create a token file to track the position of the XML content in the CUE file.
	dec.tokenFile = token.NewFile(dec.fileName, 0, len(xmlText))
	dec.tokenFile.SetLinesForContent(xmlText)

	for {
		startOffset := xmlDec.InputOffset()
		// Use RawToken to keep the prefixes as written,
		// as Token gives the namespace of each prefix instead.
		t, err := xmlDec.RawToken()
		if err == io.EOF {
			if elem := dec.currXmlElement; elem != nil {
				return nil, errors.Newf(elem.pos, "unclosed XML element <%s>", rawName(elem.xmlName))
			}
			break
		}
		if err != nil {
			return nil, err
		}
		switch xmlToken := t.(type) {
		case xml.StartElement:
			err = dec.decodeStartElement(xmlToken, startOffset)
		case xml.CharData:
			err = dec.decoderInnerText(xmlToken, startOffset)
		case xml.EndElement:
			err = dec.decodeEndElement(xmlToken, startOffset)
		}
		if err != nil {
			return nil, err
		}
		// If the XML document has ended, break out of the loop.
		if dec.astRoot != nil && dec.currXmlElement == nil {
			break
		}
	}
	if dec.astRoot == nil {
		return nil, fmt.Errorf("XML document has no root element")
	}
	return dec.astRoot, nil
}

func (dec *Decoder) decoderInnerText(xmlToken xml.CharData, contentOffset int64) error {
	// Text outside of any XML element is only allowed if it is whitespace.
	if dec.currField.field == nil {
		if isWhiteSpace(xmlToken) {
			return nil
		}
		return fmt.Errorf("text content outside of an XML element is not supported")
	}
	if dec.currInnerTextPos == token.NoPos {
		dec.currInnerTextPos = dec.tokenFile.Pos(int(contentOffset), token.NoRelPos)
		dec.currInnerTextContent = dec.currInnerTextContent[:0]
	}
	// The text may continue after a comment, a CDATA section, or the like.
	dec.currInnerTextContent = append(dec.currInnerTextContent, xmlToken...)
	return nil
}

// endInnerText finishes any pending text content of the current XML element,
// adding it to the element's value. Only whitespace is supported within
// an element which has sub-elements.
func (dec *Decoder) endInnerText(hasChildren bool) error {
	pos := dec.currInnerTextPos
	if pos == token.NoPos {
		return nil
	}
	dec.currInnerTextPos = token.NoPos
	if hasChildren {
		if !isWhiteSpace(dec.currInnerTextContent) {
			return mixedContentError()
		}
		return nil
	}
	txtLabel := ast.NewStringLabel(contentAttribute)
	ast.SetPos(txtLabel, pos)
	val := toBasicLit(string(dec.currInnerTextContent))
	ast.SetPos(val, pos)
	dec.appendToCurrFieldStruct(&ast.Field{
		Label:    txtLabel,
		Value:    val,
		TokenPos: pos,
	})
	return nil
}

func (dec *Decoder) decodeEndElement(xmlToken xml.EndElement, endOffset int64) error {
	// [xml.Decoder.RawToken] does not check that end elements match start elements.
	start := dec.currXmlElement
	pos := dec.tokenFile.Pos(int(endOffset), token.NoRelPos)
	if start == nil {
		return errors.Newf(pos, "unexpected XML end element </%s>", rawName(xmlToken.Name))
	}
	if start.xmlName != xmlToken.Name {
		return errors.Newf(pos, "XML element <%s> closed by </%s>", rawName(start.xmlName), rawName(xmlToken.Name))
	}
	// For the xmlElement hierarchy: step back up the XML hierarchy.
	if err := dec.endInnerText(len(start.children) > 0); err != nil {
		return err
	}
	dec.currXmlElement = start.parent
	// For the CUE ast: end current element, and step back up the XML hierarchy.
	if len(dec.ancestors) > 0 {
		dec.currField = dec.ancestors[len(dec.ancestors)-1]
		dec.ancestors = dec.ancestors[:len(dec.ancestors)-1]
	}
	return nil
}

func (dec *Decoder) decodeStartElement(xmlToken xml.StartElement, startOffset int64) error {
	parentXmlNode := dec.currXmlElement
	// If this is not the root node, any text content so far must be whitespace.
	if parentXmlNode != nil {
		if err := dec.endInnerText(true); err != nil {
			return err
		}
	}
	// For xmlElement hierarchy: step down the XML hierarchy.
	pos := dec.tokenFile.Pos(int(startOffset), token.NoRelPos)
	xmlNode, err := newXmlElement(xmlToken, parentXmlNode, pos)
	if err != nil {
		return err
	}
	dec.currXmlElement = xmlNode
	newElement := dec.cueFieldFromXmlElement(xmlToken, pos)
	// Covers the root node.
	if parentXmlNode == nil {
		dec.currField.assignNewCurrField(newElement)
		dec.astRoot = ast.NewStruct(dec.currField.field)
		ast.SetPos(dec.astRoot, dec.tokenFile.Pos(0, token.NoRelPos))
		return nil
	}
	parentXmlNode.children = append(parentXmlNode.children, xmlNode)
	// For the CUE ast: step down the CUE hierarchy.
	dec.ancestors = append(dec.ancestors, dec.currField)
	prefixedXmlElementName := rawName(xmlToken.Name)
	// Check if this new XML element has a name that's been seen before at the current level.
	sameNameElements := dec.currField.currFieldChildren[prefixedXmlElementName]
	if sameNameElements != nil {
		list, ok := sameNameElements.Value.(*ast.ListLit)
		// If the field's value is not a ListLit, create a new ListLit and append the existing field.
		if !ok {
			list = &ast.ListLit{Elts: []ast.Expr{sameNameElements.Value}}
			sameNameElements.Value = list
		}
		// Append the new element to the ListLit, which we now know exists.
		list.Elts = append(list.Elts, newElement.Value)
		dec.currField.assignNewCurrField(newElement)
		return nil
	}
	dec.currField.currFieldChildren[prefixedXmlElementName] = newElement
	dec.appendToCurrFieldStruct(newElement)
	dec.currField.assignNewCurrField(newElement)
	return nil
}

func (dec *Decoder) appendToCurrFieldStruct(field *ast.Field) {
	dec.currField.field.Value.(*ast.StructLit).Elts = append(dec.currField.field.Value.(*ast.StructLit).Elts, field)
}

func mixedContentError() error {
	return fmt.Errorf("text content within an XML element that has sub-elements is not supported")
}

func isWhiteSpace(b []byte) bool {
	return len(bytes.TrimSpace(b)) == 0
}

// newXmlElement returns the [xmlElement] for a start element at the given position,
// checking its namespace declarations and prefixes.
func newXmlElement(elem xml.StartElement, parent *xmlElement, pos token.Pos) (*xmlElement, error) {
	xmlNode := &xmlElement{xmlName: elem.Name, pos: pos, parent: parent, ns: predefinedNamespaces}
	if parent != nil {
		xmlNode.ns = parent.ns
	}
	cloned := false
	for _, a := range elem.Attr {
		if a.Name.Space != "xmlns" {
			continue
		}
		if a.Value == "" {
			return nil, errors.Newf(pos, "empty XML namespace for prefix %q", a.Name.Local)
		}
		if !cloned {
			xmlNode.ns = maps.Clone(xmlNode.ns)
			cloned = true
		}
		xmlNode.ns[a.Name.Local] = a.Value
	}
	if elem.Name.Space == "xmlns" {
		return nil, errors.Newf(pos, "XML element name with the reserved prefix \"xmlns\"")
	}
	if _, err := xmlNode.resolve(elem.Name); err != nil {
		return nil, err
	}
	// Attribute names must be unique by namespace and local name.
	seenAttrs := make(map[xml.Name]string, len(elem.Attr))
	for _, a := range elem.Attr {
		expanded, err := xmlNode.resolve(a.Name)
		if err != nil {
			return nil, err
		}
		attrName := rawName(a.Name)
		if seen, ok := seenAttrs[expanded]; ok {
			if seen == attrName {
				return nil, errors.Newf(pos, "duplicate XML attribute %q", attrName)
			}
			return nil, errors.Newf(pos, "XML attributes %q and %q have the same namespace and name", seen, attrName)
		}
		seenAttrs[expanded] = attrName
	}
	return xmlNode, nil
}

// resolve returns a name with its namespace prefix replaced by its namespace,
// or an error if the prefix is not in scope. Names without a prefix are returned as is,
// which is only their full namespace and name for attributes.
func (xmlNode *xmlElement) resolve(name xml.Name) (xml.Name, error) {
	if name.Space == "" {
		return name, nil
	}
	ns, ok := xmlNode.ns[name.Space]
	if !ok {
		return xml.Name{}, errors.Newf(xmlNode.pos, "undeclared XML namespace prefix %q", name.Space)
	}
	return xml.Name{Space: ns, Local: name.Local}, nil
}

// cueFieldFromXmlElement creates a new [ast.Field] to model the given xml element information
// in [xml.StartElement]. The pos represents the position
// of the beginning of the start tag of the given XML element.
func (dec *Decoder) cueFieldFromXmlElement(elem xml.StartElement, pos token.Pos) *ast.Field {
	resLabel := ast.NewStringLabel(rawName(elem.Name))
	ast.SetPos(resLabel, pos)
	resultValue := &ast.StructLit{}
	result := &ast.Field{
		Label:    resLabel,
		Value:    resultValue,
		TokenPos: pos,
	}
	// Extract attributes as children.
	for _, a := range elem.Attr {
		label := ast.NewStringLabel(attributeSymbol + rawName(a.Name))
		value := toBasicLit(a.Value)
		ast.SetPos(label, pos)
		ast.SetPos(value, pos)
		attrExpr := &ast.Field{
			Label:    label,
			Value:    value,
			TokenPos: pos,
		}
		resultValue.Elts = append(resultValue.Elts, attrExpr)
	}
	return result
}

// rawName returns the full name of an element or attribute,
// including its namespace prefix as written if it has one.
func rawName(name xml.Name) string {
	if name.Space == "" {
		return name.Local
	}
	return name.Space + ":" + name.Local
}

func toBasicLit(s string) *ast.BasicLit {
	s = strings.ReplaceAll(s, "\r", "")
	return ast.NewString(s)
}

func (cf *currFieldInfo) assignNewCurrField(field *ast.Field) {
	cf.field = field
	cf.currFieldChildren = make(map[string]*ast.Field)
}
