// Copyright 2026 The CUE Authors
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

package cue_test

import (
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/go-quicktest/qt"
)

func TestIsIncomplete(t *testing.T) {
	testCases := []struct {
		name         string
		cueValue     string
		path         string // empty means use root value
		wantErr      bool   // whether Err() should return non-nil
		isIncomplete bool
	}{{
		name:         "field not found",
		cueValue:     `a: 1`,
		path:         "b",
		wantErr:      true,
		isIncomplete: true,
	}, {
		name:         "permanent error: type conflict",
		cueValue:     `a: 1 & "foo"`,
		path:         "a",
		wantErr:      true,
		isIncomplete: false,
	}, {
		name:         "empty disjunction or([])",
		cueValue:     `#Test: or([])`,
		path:         "#Test",
		wantErr:      true,
		isIncomplete: true,
	}, {
		name:         "invalid interpolation",
		cueValue:     `input: string, x: "\(input)"`,
		path:         "x",
		wantErr:      true,
		isIncomplete: true,
	}, {
		name:     "reference to undefined field",
		cueValue: `a: b`,
		path:     "a",
		wantErr:  true,
		// This is an eval error (permanent), not incomplete
		isIncomplete: false,
	}, {
		name:         "required field missing",
		cueValue:     `a!: int`,
		path:         "a",
		wantErr:      true,
		isIncomplete: true,
	}, {
		name:         "concrete value - no error",
		cueValue:     `a: 1`,
		path:         "a",
		wantErr:      false,
		isIncomplete: false,
	}, {
		name:     "incomplete disjunction - no error from Err()",
		cueValue: `{#item: _, a: #item.b} | string`,
		path:     "",
		// This is valid CUE, just incomplete - Err() returns nil
		wantErr:      false,
		isIncomplete: false,
	}, {
		name:     "cycle - no error from Err()",
		cueValue: `a: b, b: a`,
		path:     "a",
		// Cycles are handled as valid incomplete values
		wantErr:      false,
		isIncomplete: false,
	}, {
		name:     "incomplete or with type conflict",
		cueValue: `or([]) & (1 & "foo")`,
		path:     "",
		wantErr:  true,
		// CUE prioritizes the permanent error (type conflict) over the incomplete error.
		// When both incomplete and permanent errors exist, Err() returns only the
		// permanent error. This means IsIncomplete correctly returns false.
		isIncomplete: false,
	}, {
		name:     "struct with incomplete and permanent child errors",
		cueValue: `{a: or([]), b: 1 & "foo"}`,
		path:     "",
		wantErr:  true,
		// Root Err() shows only the permanent error from field b, not the
		// incomplete error from field a. CUE prioritizes permanent errors.
		isIncomplete: false,
	}, {
		name:         "reference not found at root",
		cueValue:     `x`,
		path:         "",
		wantErr:      true,
		isIncomplete: false, // eval error
	}, {
		name:         "disjunction all arms fail",
		cueValue:     `(1 | 2) & "foo"`,
		path:         "",
		wantErr:      true,
		isIncomplete: false, // eval error from failed disjunction
	}}

	ctx := cuecontext.New()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			v := ctx.CompileString(tc.cueValue)
			if tc.path != "" {
				v = v.LookupPath(cue.ParsePath(tc.path))
			}

			err := v.Err()

			if tc.wantErr {
				qt.Assert(t, qt.IsNotNil(err), qt.Commentf("expected error for %q path %q", tc.cueValue, tc.path))
			} else {
				qt.Assert(t, qt.IsNil(err), qt.Commentf("expected no error for %q path %q", tc.cueValue, tc.path))
			}

			qt.Check(t, qt.Equals(cue.IsIncomplete(err), tc.isIncomplete),
				qt.Commentf("IsIncomplete mismatch for error: %v", err))
		})
	}
}
