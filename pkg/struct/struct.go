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

// Package struct defines utilities for struct types.
package structs

import (
	"cuelang.org/go/cue"
)

// MinFields validates the minimum number of fields that are part of a struct.
//
// Only fields that are part of the data model count. This excludes hidden
// fields, optional fields, and definitions.
func MinFields(object *cue.Struct, n int) (bool, error) {
	iter := object.Fields(cue.Hidden(false), cue.Optional(false))
	count := 0
	for iter.Next() {
		count++
	}
	return count >= n, nil
}

// MaxFields validates the maximum number of fields that are part of a struct.
//
// Only fields that are part of the data model count. This excludes hidden
// fields, optional fields, and definitions.
func MaxFields(object *cue.Struct, n int) (bool, error) {
	iter := object.Fields(cue.Hidden(false), cue.Optional(false))
	count := 0
	for iter.Next() {
		count++
	}
	return count <= n, nil
}
