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

package openapi

import (
	"encoding/json"

	"cuelang.org/go/cue"
)

// A Generator converts CUE to OpenAPI.
type Generator struct {
	// Info specifies the info section of the OpenAPI document. To be a valid
	// OpenAPI document, it must include at least the title and version fields.
	Info OrderedMap

	// ReferenceFunc allows users to specify an alternative representation
	// for references. An empty string tells the generator to expand the type
	// in place and, if applicable, not generate a schema for that entity.
	ReferenceFunc func(inst *cue.Instance, path []string) string

	// DescriptionFunc allows rewriting a description associated with a certain
	// field. A typical implementation compiles the description from the
	// comments obtains from the Doc method. No description field is added if
	// the empty string is returned.
	DescriptionFunc func(v cue.Value) string

	// SelfContained causes all non-expanded external references to be included
	// in this document.
	SelfContained bool

	// FieldFilter defines a regular expression of all fields to omit from the
	// output. It is only allowed to filter fields that add additional
	// constraints. Fields that indicate basic types cannot be removed. It is
	// an error for such fields to be excluded by this filter.
	// Fields are qualified by their Object type. For instance, the
	// minimum field of the schema object is qualified as Schema/minimum.
	FieldFilter string

	// ExpandReferences replaces references with actual objects when generating
	// OpenAPI Schema. It is an error for an CUE value to refer to itself
	// if this option is used.
	ExpandReferences bool
}

// Config is now Generator
// Deprecated: use Generator
type Config = Generator

// Gen generates the set OpenAPI schema for all top-level types of the
// given instance.
//
// Deprecated: use Generator.All.
func Gen(inst *cue.Instance, c *Config) ([]byte, error) {
	if c == nil {
		c = defaultConfig
	}
	all, err := c.All(inst)
	if err != nil {
		return nil, err
	}
	return json.Marshal(all)
}

// All generates an OpenAPI definition from the given instance.
//
// Note: only a limited number of top-level types are supported so far.
func (g *Generator) All(inst *cue.Instance) (*OrderedMap, error) {
	all, err := g.Schemas(inst)
	if err != nil {
		return nil, err
	}

	schemas := &OrderedMap{}
	schemas.Set("schemas", all)

	top := &OrderedMap{}
	top.Set("openapi", "3.0.0")
	top.Set("info", &g.Info)
	top.Set("components", schemas)

	return top, nil
}

// Schemas extracts component/schemas from the CUE top-level types.
func (g *Generator) Schemas(inst *cue.Instance) (*OrderedMap, error) {
	comps, err := schemas(g, inst)
	if err != nil {
		return nil, err
	}
	return comps, err
}

var defaultConfig = &Config{}

// TODO
// The conversion interprets @openapi(<entry> {, <entry>}) attributes as follows:
//
//      readOnly        sets the readOnly flag for a property in the schema
//                      only one of readOnly and writeOnly may be set.
//      writeOnly       sets the writeOnly flag for a property in the schema
//                      only one of readOnly and writeOnly may be set.
//      discriminator   explicitly sets a field as the discriminator field
//      deprecated      sets a field as deprecated
//
