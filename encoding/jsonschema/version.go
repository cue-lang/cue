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
	"strings"
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

	numJSONSchemaVersions // unknown

	// Note: The following versions stand alone: they're not in the regular JSON Schema lineage.
	VersionOpenAPI       // OpenAPI 3.0
	VersionKubernetesAPI // Kubernetes API
	VersionKubernetesCRD // Kubernetes CRD
)

const (
	openAPI     = versionSet(1 << VersionOpenAPI)
	k8sAPI      = versionSet(1 << VersionKubernetesAPI)
	k8sCRD      = versionSet(1 << VersionKubernetesCRD)
	k8s         = k8sAPI | k8sCRD
	openAPILike = openAPI | k8s
)

// is reports whether v is in the set vs.
func (v Version) is(vs versionSet) bool {
	return vs.contains(v)
}

type versionSet int

// allVersions includes all regular versions of JSON Schema.
// It does not include OpenAPI v3.0
const allVersions = versionSet(1<<numJSONSchemaVersions-1) &^ (1 << VersionUnknown)

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
	// Ignore a trailing empty fragment: it's a common error
	// to omit or supply such a fragment and it's not entirely
	// clear whether comparison should or should not
	// be sensitive to its presence or absence.
	sv = strings.TrimSuffix(sv, "#")
	// If this linear search is ever a performance issue, we could
	// build a map, but it doesn't seem worthwhile for now.
	for i := Version(1); i < numJSONSchemaVersions; i++ {
		if sv == strings.TrimSuffix(i.String(), "#") {
			return i, nil
		}
	}
	return 0, fmt.Errorf("$schema URI not recognized")
}
