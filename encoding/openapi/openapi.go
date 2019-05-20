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

// A Config defines options for mapping CUE to and from OpenAPI.
type Config struct {
	// ExpandReferences replaces references with actual objects when generating
	// OpenAPI Schema. It is an error for an CUE value to refer to itself
	// when this object is used.
	ExpandReferences bool
}

// Gen generates the set OpenAPI schema for all top-level types of the given
// instance.
//
func Gen(inst *cue.Instance, c *Config) ([]byte, error) {
	if c == nil {
		c = defaultConfig
	}
	comps, err := components(inst, c)
	if err != nil {
		return nil, err
	}
	return json.Marshal(comps)
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
