// Copyright 2026 CUE Authors
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

package filetypes_test

import (
	"slices"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/internal/filetypes"
)

// TestAspectNamesMatchTemplate enforces that the hand-maintained
// aspect-name table stays in one-to-one correspondence with the
// Boolean aspect fields of types.cue's #FileInfo template. Adding an
// aspect to either side without the other fails here, rather than
// surfacing as a validation discrepancy between the evaluator-free
// checks and the template.
func TestAspectNamesMatchTemplate(t *testing.T) {
	root := compileTypes(t)
	fi := root.LookupPath(cue.ParsePath("#FileInfo"))
	if err := fi.Err(); err != nil {
		t.Fatal(err)
	}

	var fields []string
	it, err := fi.Fields(cue.Optional(true))
	if err != nil {
		t.Fatal(err)
	}
	for it.Next() {
		if it.Value().IncompleteKind() == cue.BoolKind {
			fields = append(fields, it.Selector().Unquoted())
		}
	}
	slices.Sort(fields)

	names := slices.Clone(filetypes.AspectNamesForTest())
	slices.Sort(names)

	if !slices.Equal(fields, names) {
		t.Errorf("aspect names diverge from #FileInfo's Boolean fields:\n"+
			"  template: %v\n  table:    %v", fields, names)
	}
}
