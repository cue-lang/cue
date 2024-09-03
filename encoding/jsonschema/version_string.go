// Code generated by "stringer -type=Version -linecomment"; DO NOT EDIT.

package jsonschema

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[VersionUnknown-0]
	_ = x[VersionDraft4-1]
	_ = x[VersionDraft6-2]
	_ = x[VersionDraft7-3]
	_ = x[VersionDraft2019_09-4]
	_ = x[VersionDraft2020_12-5]
	_ = x[numJSONSchemaVersions-6]
	_ = x[VersionOpenAPI-7]
}

const _Version_name = "unknownhttp://json-schema.org/draft-04/schema#http://json-schema.org/draft-06/schema#http://json-schema.org/draft-07/schema#https://json-schema.org/draft/2019-09/schemahttps://json-schema.org/draft/2020-12/schemaunknownOpenAPI 3.0"

var _Version_index = [...]uint8{0, 7, 46, 85, 124, 168, 212, 219, 230}

func (i Version) String() string {
	if i < 0 || i >= Version(len(_Version_index)-1) {
		return "Version(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _Version_name[_Version_index[i]:_Version_index[i+1]]
}
