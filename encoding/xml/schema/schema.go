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

package schema

import (
	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/value"
)

// Option defines options for the decoder and encoder.
type Option func(*options)

type options struct {
	namespaces map[string]string // prefix -> URI
}

// WithNamespace adds a namespace prefix to URI mapping used for matching
// XML elements qualified with @xml(ns=<prefix>).
func WithNamespace(prefix, uri string) Option {
	return func(o *options) {
		if o.namespaces == nil {
			o.namespaces = make(map[string]string)
		}
		o.namespaces[prefix] = uri
	}
}

// mapping describes how a CUE struct maps to/from an XML element.
type mapping struct {
	tag      string                // XML element tag
	ns       string                // namespace prefix
	attrs    map[string]*fieldInfo // attr name -> field
	children map[string]*fieldInfo // child tag -> field
	// childOrder preserves the iteration order of children for encoding.
	childOrder []*fieldInfo
	body       *fieldInfo // text content field
}

// fieldInfo describes a single CUE field's mapping to XML.
type fieldInfo struct {
	cueName string
	xmlName string // tag name, attr name, or empty for body
	isAttr  bool
	isBody  bool
	ns      string
	isList  bool
	value   cue.Value
	msg     *mapping // non-nil for struct-typed children
}

// schemaParser handles parsing CUE schemas into mapping trees.
// The cache is shared across calls for efficiency.
type schemaParser struct {
	m    map[*adt.Vertex]*mapping
	errs errors.Error
}

func (p *schemaParser) addErr(err error) {
	p.errs = errors.Append(p.errs, errors.Promote(err, "xml"))
}

// parseSchema walks a CUE schema value, reads @xml attributes on each field,
// and builds a mapping tree. Results are cached by *adt.Vertex.
func (p *schemaParser) parseSchema(schema cue.Value) *mapping {
	_, v := value.ToInternal(schema)
	if v == nil {
		return nil
	}

	if p.m == nil {
		p.m = map[*adt.Vertex]*mapping{}
	} else if m := p.m[v]; m != nil {
		return m
	}

	m := &mapping{
		attrs:    make(map[string]*fieldInfo),
		children: make(map[string]*fieldInfo),
	}

	i, err := schema.Fields(cue.Optional(true))
	if err != nil {
		p.addErr(err)
		return nil
	}

	for i.Next() {
		name := i.Selector().Unquoted()
		val := i.Value()
		fi := &fieldInfo{
			cueName: name,
			xmlName: name, // default: CUE field name
			value:   val,
		}

		// Parse @xml attribute.
		a := val.Attribute("xml")
		if a.Err() == nil {
			parseXMLAttr(a, fi)
		}

		if fi.isBody {
			m.body = fi
			continue
		}
		if fi.isAttr {
			m.attrs[fi.xmlName] = fi
			continue
		}

		// Check if this is a list type.
		if val.IncompleteKind() == cue.ListKind {
			fi.isList = true
			// For lists, look at the element type. Try the constraint
			// first (AnyIndex), then fall back to the first concrete element.
			elem := val.LookupPath(cue.MakePath(cue.AnyIndex))
			if !elem.Exists() {
				if iter, err := val.List(); err == nil && iter.Next() {
					elem = iter.Value()
				}
			}
			if elem.Exists() && elem.IncompleteKind() == cue.StructKind {
				fi.msg = p.parseSchema(elem)
			}
			fi.value = val
		} else if val.IncompleteKind() == cue.StructKind {
			fi.msg = p.parseSchema(val)
		}

		m.children[fi.xmlName] = fi
		m.childOrder = append(m.childOrder, fi)
	}

	p.m[v] = m
	return m
}

// parseXMLAttr extracts XML mapping info from an @xml attribute.
func parseXMLAttr(a cue.Attribute, fi *fieldInfo) {
	for idx := 0; idx < a.NumArgs(); idx++ {
		key, val := a.Arg(idx)
		switch {
		case key == "body" && val == "":
			fi.isBody = true
		case key == "attr":
			fi.isAttr = true
			if val != "" {
				fi.xmlName = val
			}
		case key == "tag":
			if val != "" {
				fi.xmlName = val
			}
		case key == "ns":
			if val != "" {
				fi.ns = val
			}
		}
	}
}
