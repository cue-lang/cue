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

package jsonschema

import (
	"cuelang.org/go/cue"
)

// Meta constraints

func constraintID(key string, n cue.Value, s *state) {
	// URL: https://domain.com/schemas/foo.json
	// anchors: #identifier
	//
	// TODO: mark anchors

	// Resolution is relative to parent $id
	// https://tools.ietf.org/html/draft-handrews-json-schema-02#section-8.2.2
	u := s.resolveURI(n)
	if u == nil {
		return
	}

	if u.Fragment != "" {
		// TODO do not use StrictFeatures for this. The specification is clear:
		// before 2019-09, IDs could contain plain-name fragments;
		// (see https://json-schema.org/draft-07/draft-handrews-json-schema-01#rfc.section.5)
		// afterwards, $anchor was reserved for that purpose.
		if s.cfg.StrictFeatures {
			s.errf(n, "$id URI may not contain a fragment")
		}
		return
	}
	s.id = u
}

// constraintSchema implements $schema, which
// identifies this as a JSON schema and specifies its version.
func constraintSchema(key string, n cue.Value, s *state) {
	if !s.isRoot && !vfrom(VersionDraft2019_09).contains(s.schemaVersion) {
		// Before 2019-09, the $schema keyword was not allowed
		// to appear anywhere but the root.
		s.errf(n, "$schema can only appear at the root in JSON Schema version %v", s.schemaVersion)
		return
	}
	str, ok := s.strValue(n)
	if !ok {
		// If there's no $schema value, use the default.
		return
	}
	sv, err := ParseVersion(str)
	if err != nil {
		s.errf(n, "invalid $schema URL %q: %v", str, err)
		return
	}
	s.schemaVersionPresent = true
	s.schemaVersion = sv
}

func constraintTODO(key string, n cue.Value, s *state) {
	if s.cfg.StrictFeatures {
		s.errf(n, `keyword %q not yet implemented`, key)
	}
}
