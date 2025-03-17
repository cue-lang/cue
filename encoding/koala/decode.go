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

// Package koala converts XML to and from CUE as described here: https://github.com/cue-lang/cue/discussions/3776
//
// WARNING: THIS PACKAGE IS EXPERIMENTAL.
// ITS API MAY CHANGE AT ANY TIME.
package koala

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strings"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/token"
)

// koala is an XML encoding for CUE described here: https://github.com/cue-lang/cue/discussions/3776
// Decoder implements the decoding state.
type Decoder struct {
	reader io.Reader
	//required to find attribute and content offsets
	xmlText   string
	fileName  string
	tokenFile *token.File

	// current XML element being processed
	currXmlElement *XMLElement

	// Properties below relate to ast representation of XML document
	// a description of this model can be found at https://github.com/cue-lang/cue/discussions/3776
	astRoot *ast.StructLit
	//CUE model of ancestors of current XML element being processed
	ancestors []*ast.Field
	//CUE model of current XML element
	currField *ast.Field
	//CUE model of current XML element's inner content ($$ attribute)
	currInnerText *ast.Field
}

// models an XML Element hierarchy
// used for tracking namespace prefixes
type XMLElement struct {
	xmlName                 xml.Name
	attr                    []xml.Attr
	parent                  *XMLElement
	children                []*XMLElement
	textContentIsWhiteSpace bool
}

// the prefix used to model the inner text content within an XML element
const ContentAttribute string = "$$"

// the prefix used to model each attribute of an XML element
const AttributeSymbol string = "$"

// NewDecoder creates a decoder from a stream of XML input.
func NewDecoder(fileName string, reader io.Reader) *Decoder {
	return &Decoder{reader: reader, fileName: fileName}
}

// Decode parses the input stream as XML and converts it to a CUE [ast.Expr].
// The input stream is taken from the [Decoder] and consumed.
// If an error is encountered in the decoding process, this function returns it.
func (dec *Decoder) Decode() (ast.Expr, error) {
	data, reader, err := bytesFromReader(dec.reader)
	if err != nil {
		return nil, err
	}
	//required to find attribute and content offsets
	dec.xmlText = string(data)
	dec.reader = reader
	//create a token file to track the position of the XML content in the CUE file
	dec.tokenFile = token.NewFile(dec.fileName, 0, len(data))
	dec.tokenFile.SetLinesForContent(data)
	xmlDec := xml.NewDecoder(dec.reader)
	for {
		t, err := xmlDec.Token()
		if err != nil && err != io.EOF {
			return nil, err
		}
		if t == nil {
			break
		}
		switch xmlToken := t.(type) {
		case xml.StartElement:
			err = dec.decodeStartElement(xmlToken, xmlDec)
		case xml.CharData:
			err = dec.decoderInnerText(xmlToken, xmlDec)
		case xml.EndElement:
			err = dec.decodeEndElement(xmlToken)
		}
		if err != nil {
			return nil, err
		}
	}
	return dec.astRoot, nil
}

func (dec *Decoder) decoderInnerText(xmlToken xml.CharData, xmlDec *xml.Decoder) error {
	//if this is text content within an XML element
	textContent := string(xml.CharData(xmlToken))
	if dec.currField != nil {
		contentOffset := dec.contentOffset(int(xmlDec.InputOffset()))
		txtContentPosition := dec.tokenFile.Pos(contentOffset, token.NoRelPos)
		txtLabel := ast.NewString(ContentAttribute)
		txtLabel.ValuePos = txtContentPosition
		val := convertToBasicLit(textContent)
		val.ValuePos = txtContentPosition
		textContentNode := &ast.Field{
			Label:    txtLabel,
			Value:    val,
			TokenPos: dec.tokenFile.Pos(contentOffset, token.NoRelPos),
		}
		dec.currInnerText = textContentNode
		dec.currXmlElement.textContentIsWhiteSpace = isWhiteSpace(textContent)
		return nil
	} else {
		if isWhiteSpace(textContent) {
			return nil
		}
		return fmt.Errorf("text content outside of an XML element is not supported")
	}
}

func (dec *Decoder) decodeEndElement(xmlToken xml.EndElement) error {
	//should match the start element name
	if dec.currXmlElement.xmlName.Local != xmlToken.Name.Local {
		return fmt.Errorf("mismatched start and end element names: %s and %s", dec.currXmlElement.xmlName.Local, xmlToken.Name.Local)
	}
	//if there is text content within the element, add it to the element's value
	if dec.currXmlElement != nil && dec.currInnerText != nil {
		//only support text content within an element that has no sub-elements
		if len(dec.currXmlElement.children) == 0 {
			err := dec.addFieldToCurrElement(dec.currInnerText)
			if err != nil {
				return err
			}
			dec.currInnerText = nil
		} else {
			//if there is text content within an element that has sub-elements, return an error
			if len(dec.currXmlElement.children) > 0 && !dec.currXmlElement.textContentIsWhiteSpace {
				return mixedContentError()
			}
		}
	}
	//XMLElement: step back up the XML hierarchy
	if dec.currXmlElement != nil {
		dec.currXmlElement = dec.currXmlElement.parent
	}
	//CUE ast: end current element, and step back up the XML hierarchy
	if len(dec.ancestors) > 0 {
		dec.currField = dec.ancestors[len(dec.ancestors)-1]
		dec.ancestors = dec.ancestors[:len(dec.ancestors)-1]
	}
	return nil
}

func (dec *Decoder) decodeStartElement(xmlToken xml.StartElement, xmlDec *xml.Decoder) error {
	//if this is the root node
	if dec.currField == nil {
		dec.currXmlElement = &XMLElement{xmlName: xmlToken.Name, attr: xmlToken.Attr, children: []*XMLElement{}}
		cueElement, err := dec.cueFieldFromXmlElement(xmlToken, int(xmlDec.InputOffset()), dec.currXmlElement)
		if err != nil {
			return err
		}
		dec.currField = cueElement
		dec.astRoot = ast.NewStruct(dec.currField)
		ast.SetPos(dec.astRoot, dec.tokenFile.Pos(0, token.NoRelPos))
	} else {
		if dec.currInnerText != nil && !dec.currXmlElement.textContentIsWhiteSpace {
			return mixedContentError()
		}

		//clear any whitespace text content
		dec.currInnerText = nil

		//XMLElement: step down the XML hierarchy
		parentXmlNode := dec.currXmlElement
		dec.currXmlElement = &XMLElement{xmlName: xmlToken.Name, attr: xmlToken.Attr, parent: parentXmlNode, children: []*XMLElement{}}
		parentXmlNode.children = append(parentXmlNode.children, dec.currXmlElement)
		//CUE ast: step down the CUE hierarchy
		dec.ancestors = append(dec.ancestors, dec.currField)
		newElement, err := dec.cueFieldFromXmlElement(xmlToken, int(xmlDec.InputOffset()), dec.currXmlElement)
		if err != nil {
			return err
		}
		//check if this new XML element has a name that has seen before at the current level
		xmlElementProperties, err := elementProperties(dec.currField)
		if err != nil {
			return err
		}
		for _, elt := range xmlElementProperties {
			prefixedXmlElementName, err := prefixedElementName(xmlToken, dec.currXmlElement)
			if err != nil {
				return err
			}
			fieldElementName, err := elementNameFromField(elt)
			if err != nil {
				return err
			}
			//if the new element has the same name as an existing element at this level add it to a list for that element name
			if fieldElementName == ast.NewString(prefixedXmlElementName).Value {
				//if the field's value is not a ListLit, create a new ListLit and append the existing field
				if _, ok := elt.(*ast.Field).Value.(*ast.ListLit); !ok {
					elt.(*ast.Field).Value = &ast.ListLit{Elts: []ast.Expr{elt.(*ast.Field).Value}}
				}
				//append the new element to the ListLit, which we now know exists
				elt.(*ast.Field).Value.(*ast.ListLit).Elts = append(elt.(*ast.Field).Value.(*ast.ListLit).Elts, newElement.Value)
				dec.currField = newElement
				return nil
			}
		}
		dec.currField.Value.(*ast.StructLit).Elts = append(xmlElementProperties, newElement)
		dec.currField = newElement
	}
	return nil
}

func elementProperties(field *ast.Field) ([]ast.Decl, error) {
	err := fmt.Errorf("could not find element properties")
	if field == nil || field.Value == nil {
		return nil, err
	}
	structLit, ok := field.Value.(*ast.StructLit)
	if !ok {
		return nil, err
	}
	return structLit.Elts, nil
}

func elementNameFromField(elt ast.Decl) (string, error) {
	err := fmt.Errorf("could not find element name")
	field, ok := elt.(*ast.Field)
	if !ok || field.Label == nil {
		return "", err
	}
	basicLit, ok := field.Label.(*ast.BasicLit)
	if !ok || basicLit.Value == "" {
		return "", err
	}
	return basicLit.Value, nil
}

func mixedContentError() error {
	return fmt.Errorf("text content within an XML element that has sub-elements is not supported")
}

func isWhiteSpace(s string) bool {
	return regexp.MustCompile(`^[\s\r\n]*$`).MatchString(s)
}

// attributeOffsets returns the offset of the attribute key and value in the XML text, in that order.
// The containing element offset is the offset of the end of the element that contains the attribute.
func (dec *Decoder) attributeOffsets(attribute xml.Attr, startElement xml.StartElement, containingElementOffset int) (int, int, error) {
	//find the starting index of the element
	elementStartIdx := containingElementOffset - 1
	for elementStartIdx >= 0 && dec.xmlText[elementStartIdx] != '<' {
		elementStartIdx--
	}
	if elementStartIdx == -1 {
		return -1, -1, fmt.Errorf("could not find start of element")
	}
	elementText := dec.xmlText[elementStartIdx:containingElementOffset]
	//get the full attribute name including the namespace prefix
	attrName, err := prefixedAttrName(attribute, startElement, dec.currXmlElement)
	if err != nil {
		return -1, -1, err
	}
	// find the start index of the attribute key
	// including the attribute start quote in the search ensures that we are not matching a substring of another attribute
	re := regexp.MustCompile(`\s+(` + attrName + `\s*=\s*["'])`)
	matches := re.FindStringIndex(elementText)
	offsetFindErr := fmt.Errorf("could not find attribute %s in element %s", attrName, startElement.Name.Local)
	if matches == nil {
		return -1, -1, offsetFindErr
	}
	attrKeyIndex := matches[0]
	//increment the attrKeyIndex for each space found before the attribute name
	attrKeyIndex += bytes.IndexFunc([]byte(elementText[attrKeyIndex:]), func(r rune) bool { return r != ' ' })
	//find the start index of the value
	attrValueIndex := bytes.IndexByte([]byte(elementText[attrKeyIndex:]), '"') + attrKeyIndex
	if attrKeyIndex == -1 || attrValueIndex == -1 {
		return -1, -1, offsetFindErr
	}
	return elementStartIdx + attrKeyIndex, elementStartIdx + attrValueIndex, nil
}

// find the start of the $$content that ends at the endElementOffset
func (dec *Decoder) contentOffset(endElementOffset int) int {
	//find the start of the content of the element
	contentStartIdx := endElementOffset
	for i := endElementOffset - 1; i > 0; i-- {
		if dec.xmlText[i] == '>' {
			return i + 1
		}
	}
	return contentStartIdx
}

// create a new ast.Field to model the XML element
func (dec *Decoder) cueFieldFromXmlElement(elem xml.StartElement, offset int, xmlNode *XMLElement) (*ast.Field, error) {
	elementName, err := prefixedElementName(elem, xmlNode)
	if err != nil {
		return nil, err
	}
	resLabel := ast.NewString(elementName)
	resLabel.ValuePos = dec.tokenFile.Pos(offset, token.NoRelPos)
	result := &ast.Field{
		Label:    resLabel,
		Value:    &ast.StructLit{},
		TokenPos: dec.tokenFile.Pos(offset, token.NoRelPos),
	}
	// Extract attributes as children
	for _, a := range elem.Attr {
		attrName, err := prefixedAttrName(a, elem, xmlNode)
		if err != nil {
			return nil, err
		}
		label := ast.NewString(AttributeSymbol + attrName)
		value := convertToBasicLit(a.Value)
		attrKeyOffset, attrValOffset, err := dec.attributeOffsets(a, elem, offset)
		if err != nil {
			return nil, err
		}
		label.ValuePos = dec.tokenFile.Pos(attrKeyOffset, token.NoRelPos)
		value.ValuePos = dec.tokenFile.Pos(attrValOffset, token.NoRelPos)
		attrExpr := &ast.Field{
			Label:    label,
			Value:    value,
			TokenPos: dec.tokenFile.Pos(attrKeyOffset, token.NoRelPos),
		}
		result.Value.(*ast.StructLit).Elts = append(result.Value.(*ast.StructLit).Elts, attrExpr)
	}
	return result, nil
}

// return the name of an element, including its namespace prefix if it has one; but without namespace prefix if it is "xmlns"
func prefixedElementName(elem xml.StartElement, xmlNode *XMLElement) (string, error) {
	elementName := elem.Name.Local
	if elem.Name.Space != "" {
		prefixNS, err := nsPrefix(elem.Name.Space, elem.Attr, xmlNode)
		if err != nil {
			return elementName, err
		}
		if prefixNS != "xmlns" {
			elementName = prefixNS + ":" + elem.Name.Local
		}
	}
	return elementName, nil
}

// return the name of an attribute, including its namespace prefix if it has one
func prefixedAttrName(a xml.Attr, elem xml.StartElement, xmlNode *XMLElement) (string, error) {
	attrName := a.Name.Local
	if a.Name.Space != "" {
		prefix, err := nsPrefix(a.Name.Space, elem.Attr, xmlNode)
		if err != nil {
			return attrName, err
		}
		attrName = prefix + ":" + a.Name.Local
	}
	return attrName, nil
}

func convertToBasicLit(s string) *ast.BasicLit {
	//discard carriage returns from s
	s = strings.ReplaceAll(s, "\r", "")
	return ast.NewString(s)
}

// find the prefix label for a given namespace by looking at the current node's attributes and then
// walking up the hierarchy of XML nodes
func nsPrefix(nameSpace string, attributes []xml.Attr, xmlNode *XMLElement) (string, error) {
	//when the prefix is xmlns, then the namespace is xmlns according to the golang XML parser
	if nameSpace == "xmlns" {
		return "xmlns", nil
	}
	for _, attr := range attributes {
		if attr.Value == nameSpace {
			return attr.Name.Local, nil
		}
	}
	if xmlNode != nil {
		if xmlNode.parent != nil {
			return nsPrefix(nameSpace, xmlNode.parent.attr, xmlNode.parent)
		}
	}
	return "", fmt.Errorf("could not find prefix for namespace %s", nameSpace)
}

func bytesFromReader(r io.Reader) ([]byte, io.Reader, error) {
	//read all bytes from r
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, nil, err
	}
	//create reader from bytes
	reader := bytes.NewReader(data)
	return data, reader, nil
}

func (dec *Decoder) addFieldToCurrElement(field *ast.Field) error {
	if dec.currField == nil {
		return fmt.Errorf("current field is nil")
	}
	if dec.currField.Value == nil {
		return fmt.Errorf("current field value is nil")
	}
	structLit, ok := dec.currField.Value.(*ast.StructLit)
	if !ok {
		return fmt.Errorf("current field value is not a StructLit")
	}
	dec.currField.Value.(*ast.StructLit).Elts = append(structLit.Elts, field)
	return nil
}
