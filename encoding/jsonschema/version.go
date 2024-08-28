// Copyright 2024 CUE Authors
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
	"fmt"
)

//go:generate go run golang.org/x/tools/cmd/stringer -type=Version -linecomment

type Version int

const (
	VersionUnknown Version = iota // unknown
	VersionDraft4                 // http://json-schema.org/draft-04/schema#
	// Note: draft 5 never existed and should not be used.
	VersionDraft6       // http://json-schema.org/draft-06/schema#
	VersionDraft7       // http://json-schema.org/draft-07/schema#
	VersionDraft2019_09 // https://json-schema.org/draft/2019-09/schema
	VersionDraft2020_12 // https://json-schema.org/draft/2020-12/schema

	numVersions // unknown
)

type versionSet int

const allVersions = versionSet(1<<numVersions-1) &^ (1 << VersionUnknown)

// contains reports whether m contains the version v.
func (m versionSet) contains(v Version) bool {
	return (m & vset(v)) != 0
}

// vset returns the version set containing exactly v.
func vset(v Version) versionSet {
	return 1 << v
}

// vfrom returns the set of all versions starting at v.
func vfrom(v Version) versionSet {
	return allVersions &^ (vset(v) - 1)
}

// vbetween returns the set of all versions between
// v0 and v1 inclusive.
func vbetween(v0, v1 Version) versionSet {
	return vfrom(v0) & vto(v1)
}

// vto returns the set of all versions up to
// and including v.
func vto(v Version) versionSet {
	return allVersions & (vset(v+1) - 1)
}

// ParseVersion parses a version URI that defines a JSON Schema version.
func ParseVersion(sv string) (Version, error) {
	// If this linear search is ever a performance issue, we could
	// build a map, but it doesn't seem worthwhile for now.
	for i := Version(1); i < numVersions; i++ {
		if sv == i.String() {
			return i, nil
		}
	}
	return 0, fmt.Errorf("$schema URI not recognized")
}
