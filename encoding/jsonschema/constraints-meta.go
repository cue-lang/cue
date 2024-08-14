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
	// TODO: mark identifiers.

	// Draft-06 renamed the id field to $id.
	if key == "id" {
		// old-style id field.
		if s.schemaVersion >= versionDraft06 {
			if s.cfg.Strict {
				s.warnf(n.Pos(), "use of old-style id field not allowed in schema version %v", s.schemaVersion)
			}
			return
		}
	} else {
		// new-style $id field
		if s.schemaVersion < versionDraft07 {
			if s.cfg.Strict {
				s.warnf(n.Pos(), "use of $id not allowed in older schema version %v", s.schemaVersion)
			}
			return
		}
	}

	// Resolution must be relative to parent $id
	// https://tools.ietf.org/html/draft-handrews-json-schema-02#section-8.2.2
	u := s.resolveURI(n)
	if u == nil {
		return
	}

	if u.Fragment != "" {
		if s.cfg.Strict {
			s.errf(n, "$id URI may not contain a fragment")
		}
		return
	}
	s.id = u
	s.idPos = n.Pos()
}

func constraintSchema(key string, n cue.Value, s *state) {
	// Identifies this as a JSON schema and specifies its version.
	// TODO: extract version.

	s.schemaVersion = versionDraft07 // Reasonable default version.
	str, ok := s.strValue(n)
	if !ok {
		// If there's no $schema value, use the default.
		return
	}
	sv, err := parseSchemaVersion(str)
	if err != nil {
		s.errf(n, "invalid $schema URL %q: %v", str, err)
		return
	}
	s.schemaVersionPresent = true
	s.schemaVersion = sv
}
