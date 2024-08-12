package jsonschema

import (
	"fmt"
)

//go:generate go run golang.org/x/tools/cmd/stringer -type=schemaVersion -linecomment

type schemaVersion int

const (
	versionUnknown schemaVersion = iota // unknown
	versionDraft04                      // http://json-schema.org/draft-04/schema#
	versionDraft05                      //	http://json-schema.org/draft-05/schema#
	versionDraft06                      //	http://json-schema.org/draft-06/schema#
	versionDraft07                      //	http://json-schema.org/draft-07/schema#
	version2019_09                      // https://json-schema.org/draft/2019-09/schema
	version2020_12                      // https://json-schema.org/draft/2020-12/schema

	numVersions // unknown
)

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
