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

// Package koala converts XML to and from CUE as described here: https://github.com/cue-lang/cue/discussions/3776.
//
// WARNING: THIS PACKAGE IS EXPERIMENTAL.
// ITS API MAY CHANGE AT ANY TIME.
package koala

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"
	"unicode"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

// Decoder implements the decoding state.
type Decoder struct {
	xmlDec    *xml.Decoder
	fileName  string
	tokenFile *token.File

	// current XML element being processed.
	currXmlElement *xmlElement

	// Properties below relate to ast representation of XML document
	// a description of this model can be found at https://github.com/cue-lang/cue/discussions/3776.
	astRoot *ast.StructLit
	// CUE model of ancestors of current XML element being processed.
	ancestors []currFieldInfo
	// CUE model of current XML element being processed.
	currField currFieldInfo
	// CUE model of current XML element's inner content ($$ attribute).
	currInnerText *ast.Field
	// Decode can only run once for a Decoder instance.
	decoderRan bool
}

// The currFieldInfo type encapsulates details of the CUE field for the current XML element being processed.
type currFieldInfo struct {
	// CUE model of current XML element.
	field *ast.Field
	// Running map of the current field's children.
	currFieldChildren map[string]*ast.Field
}

// The xmlElement type models an XML Element hierarchy.
// It is used for tracking namespace prefixes.
type xmlElement struct {
	xmlName                 xml.Name
	attr                    []xml.Attr
	parent                  *xmlElement
	children                []*xmlElement
	textContentIsWhiteSpace bool
}

// The prefix used to model the inner text content within an XML element.
const contentAttribute string = "$$"

// The prefix used to model each attribute of an XML element.
const attributeSymbol string = "$"

// NewDecoder creates a decoder from a stream of XML input.
func NewDecoder(fileName string, r io.Reader) (*Decoder, error) {
	xmlText, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	reader := bytes.NewReader(xmlText)
	xmlDec := xml.NewDecoder(reader)
	dec := &Decoder{xmlDec: xmlDec, fileName: fileName}

	// Create a token file to track the position of the XML content in the CUE file.
	dec.tokenFile = token.NewFile(dec.fileName, 0, len(xmlText))
	dec.tokenFile.SetLinesForContent(xmlText)
	return dec, nil
}

// Decode parses the input stream as XML and converts it to a CUE [ast.Expr].
// The input stream is taken from the [Decoder] and consumed.
func (dec *Decoder) Decode() (ast.Expr, error) {
	if dec.decoderRan {
		return nil, io.EOF
	}
	dec.decoderRan = true
	for {
		startOffset := dec.xmlDec.InputOffset()
		t, err := dec.xmlDec.Token()
		if err == io.EOF {
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
			err = dec.decodeEndElement()
		}
		if err != nil {
			return nil, err
		}
		// If the XML document has ended, break out of the loop.
		if dec.astRoot != nil && dec.currXmlElement == nil {
			break
		}
	}
	return dec.astRoot, nil
}

func (dec *Decoder) decoderInnerText(xmlToken xml.CharData, contentOffset int64) error {
	// If this is text content within an XML element.
	textContent := string(xml.CharData(xmlToken))
	if dec.currField.field == nil {
		if isWhiteSpace(textContent) {
			return nil
		}
		return fmt.Errorf("text content outside of an XML element is not supported")
	}
	txtContentPosition := dec.tokenFile.Pos(int(contentOffset), token.NoRelPos)
	txtLabel := ast.NewString(contentAttribute)
	txtLabel.ValuePos = txtContentPosition
	val := toBasicLit(textContent)
	val.ValuePos = txtContentPosition
	textContentNode := &ast.Field{
		Label:    txtLabel,
		Value:    val,
		TokenPos: dec.tokenFile.Pos(int(contentOffset), token.NoRelPos),
	}
	dec.currInnerText = textContentNode
	dec.currXmlElement.textContentIsWhiteSpace = isWhiteSpace(textContent)
	return nil
}

func (dec *Decoder) decodeEndElement() error {
	// If there is text content within the element, add it to the element's value.
	if dec.currXmlElement != nil && dec.currInnerText != nil {
		// Only support text content within an element that has no sub-elements.
		if len(dec.currXmlElement.children) == 0 {
			dec.currField.field.Value.(*ast.StructLit).Elts = append(dec.currField.field.Value.(*ast.StructLit).Elts, dec.currInnerText)
			dec.currInnerText = nil
		} else if len(dec.currXmlElement.children) > 0 && !dec.currXmlElement.textContentIsWhiteSpace {
			// If there is text content within an element that has sub-elements, return an error.
			return mixedContentError()
		}
	}
	// For the xmlElement hierarchy: step back up the XML hierarchy.
	if dec.currXmlElement != nil {
		dec.currXmlElement = dec.currXmlElement.parent
	}
	// Fpr the CUE ast: end current element, and step back up the XML hierarchy.
	if len(dec.ancestors) > 0 {
		dec.currField = dec.ancestors[len(dec.ancestors)-1]
		dec.ancestors = dec.ancestors[:len(dec.ancestors)-1]
	}
	return nil
}

func (dec *Decoder) decodeStartElement(xmlToken xml.StartElement, startOffset int64) error {
	// Covers the root node.
	if dec.currField.field == nil {
		dec.currXmlElement = &xmlElement{xmlName: xmlToken.Name, attr: xmlToken.Attr}
		cueElement, err := dec.cueFieldFromXmlElement(xmlToken, dec.currXmlElement, startOffset)
		if err != nil {
			return err
		}
		dec.currField.assignNewCurrField(cueElement)
		dec.astRoot = ast.NewStruct(dec.currField.field)
		ast.SetPos(dec.astRoot, dec.tokenFile.Pos(0, token.NoRelPos))
		return nil
	}
	// If this is not the root node, check if there is text content within the element.
	if dec.currInnerText != nil && !dec.currXmlElement.textContentIsWhiteSpace {
		return mixedContentError()
	}
	// Clear any whitespace text content.
	dec.currInnerText = nil
	// For xmlElement hierarchy: step down the XML hierarchy.
	parentXmlNode := dec.currXmlElement
	dec.currXmlElement = &xmlElement{xmlName: xmlToken.Name, attr: xmlToken.Attr, parent: parentXmlNode}
	parentXmlNode.children = append(parentXmlNode.children, dec.currXmlElement)
	// For the CUE ast: step down the CUE hierarchy.
	dec.ancestors = append(dec.ancestors, dec.currField)
	newElement, err := dec.cueFieldFromXmlElement(xmlToken, dec.currXmlElement, startOffset)
	if err != nil {
		return err
	}

	xmlElementProperties := dec.currField.field.Value.(*ast.StructLit).Elts

	// Check if this new XML element has a name that's been seen before at the current level.
	prefixedXmlElementName := prefixedElementName(xmlToken, dec.currXmlElement)
	sameNameElements := dec.currField.currFieldChildren[prefixedXmlElementName]
	if sameNameElements != nil {
		// If the field's value is not a ListLit, create a new ListLit and append the existing field.
		if _, ok := sameNameElements.Value.(*ast.ListLit); !ok {
			sameNameElements.Value = &ast.ListLit{Elts: []ast.Expr{sameNameElements.Value}}
		}
		// Append the new element to the ListLit, which we now know exists.
		sameNameElements.Value.(*ast.ListLit).Elts = append(sameNameElements.Value.(*ast.ListLit).Elts, newElement.Value)
		dec.currField.assignNewCurrField(newElement)
		return nil
	}
	dec.currField.currFieldChildren[prefixedXmlElementName] = newElement
	dec.currField.field.Value.(*ast.StructLit).Elts = append(xmlElementProperties, newElement)
	dec.currField.assignNewCurrField(newElement)
	return nil
}

func mixedContentError() error {
	return fmt.Errorf("text content within an XML element that has sub-elements is not supported")
}

func isWhiteSpace(s string) bool {
	for _, r := range s {
		if !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}

// The cueFieldFromXmlElement function creates a new ast.Field to model the given xml element information in [xml.StartElement] and [xmlElement].
// The startOffset represents the offset for the beginning of the start tag of the given XML element.
func (dec *Decoder) cueFieldFromXmlElement(elem xml.StartElement, xmlNode *xmlElement, startOffset int64) (*ast.Field, error) {
	elementName := prefixedElementName(elem, xmlNode)
	resLabel := ast.NewString(elementName)
	resLabel.ValuePos = dec.tokenFile.Pos(int(startOffset), token.NoRelPos)
	resultValue := &ast.StructLit{}
	result := &ast.Field{
		Label:    resLabel,
		Value:    resultValue,
		TokenPos: dec.tokenFile.Pos(int(startOffset), token.NoRelPos),
	}
	// Extract attributes as children.
	for _, a := range elem.Attr {
		attrName := prefixedAttrName(a, elem, xmlNode)
		label := ast.NewString(attributeSymbol + attrName)
		value := toBasicLit(a.Value)
		label.ValuePos = dec.tokenFile.Pos(int(startOffset), token.NoRelPos)
		value.ValuePos = dec.tokenFile.Pos(int(startOffset), token.NoRelPos)
		attrExpr := &ast.Field{
			Label:    label,
			Value:    value,
			TokenPos: dec.tokenFile.Pos(int(startOffset), token.NoRelPos),
		}
		resultValue.Elts = append(result.Value.(*ast.StructLit).Elts, attrExpr)
	}
	return result, nil
}

// The prefixedElementName function returns the full name of an element, including its namespace prefix if it has one; but without namespace prefix if it is "xmlns".
func prefixedElementName(elem xml.StartElement, xmlNode *xmlElement) string {
	elementName := elem.Name.Local
	if elem.Name.Space != "" {
		prefixNS := nsPrefix(elem.Name.Space, elem.Attr, xmlNode)
		if prefixNS != "xmlns" {
			elementName = prefixNS + ":" + elem.Name.Local
		}
	}
	return elementName
}

// The prefixedAttrName function returns the full name of an attribute, including its namespace prefix if it has one.
func prefixedAttrName(a xml.Attr, elem xml.StartElement, xmlNode *xmlElement) string {
	attrName := a.Name.Local
	if a.Name.Space != "" {
		prefix := nsPrefix(a.Name.Space, elem.Attr, xmlNode)
		attrName = prefix + ":" + a.Name.Local
	}
	return attrName
}

// The toBasicLit function creates a new ast.BasicLit with the given string value and discards carriage returns.
func toBasicLit(s string) *ast.BasicLit {
	s = strings.ReplaceAll(s, "\r", "")
	return ast.NewString(s)
}

// The nsPrefix function finds the prefix label for a given namespace by looking at the current node's attributes and then
// walking up the hierarchy of XML nodes.
func nsPrefix(nameSpace string, attributes []xml.Attr, xmlNode *xmlElement) string {
	// When the prefix is xmlns, then the namespace is xmlns according to the golang XML parser.
	if nameSpace == "xmlns" {
		return "xmlns"
	}
	for _, attr := range attributes {
		if attr.Value == nameSpace {
			return attr.Name.Local
		}
	}
	if xmlNode != nil {
		if xmlNode.parent != nil {
			return nsPrefix(nameSpace, xmlNode.parent.attr, xmlNode.parent)
		}
	}
	panic("could not find prefix for namespace " + nameSpace)
}

func (cf *currFieldInfo) assignNewCurrField(field *ast.Field) {
	cf.field = field
	cf.currFieldChildren = make(map[string]*ast.Field)
}
