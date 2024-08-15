package jsonschema

import (
	"fmt"
)

//go:generate go run golang.org/x/tools/cmd/stringer -type=schemaVersion -linecomment

type schemaVersion int

const (
	versionUnknown schemaVersion = iota // unknown
	versionDraft04                      // http://json-schema.org/draft-04/schema#
	// Note: draft 05 never existed and should not be used.
	versionDraft06 // http://json-schema.org/draft-06/schema#
	versionDraft07 // http://json-schema.org/draft-07/schema#
	version2019_09 // https://json-schema.org/draft/2019-09/schema
	version2020_12 // https://json-schema.org/draft/2020-12/schema

	numVersions // unknown
)

const defaultVersion = versionDraft07

type versionSet int

const allVersions = versionSet(1<<numVersions-1) &^ (1 << versionUnknown)

// contains reports whether m contains the version v.
func (m versionSet) contains(v schemaVersion) bool {
	return (m & vset(v)) != 0
}

// vset returns the version set containing exactly v.
func vset(v schemaVersion) versionSet {
	return 1 << v
}

// vfrom returns the set of all versions starting at v.
func vfrom(v schemaVersion) versionSet {
	return allVersions &^ (vset(v) - 1)
}

// vbetween returns the set of all versions between
// v0 and v1 inclusive.
func vbetween(v0, v1 schemaVersion) versionSet {
	return vfrom(v0) & vto(v1)
}

// vto returns the set of all versions up to
// and including v.
func vto(v schemaVersion) versionSet {
	return allVersions & (vset(v+1) - 1)
}

func parseSchemaVersion(sv string) (schemaVersion, error) {
	// If this linear search is ever a performance issue, we could
	// build a map, but it doesn't seem worthwhile for now.
	for i := schemaVersion(1); i < numVersions; i++ {
		if sv == i.String() {
			return i, nil
		}
	}
	return 0, fmt.Errorf("$schema URI not recognized")
}
