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
	"encoding/json"
	stderrs "errors"
	"fmt"
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
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

func TestIsIncompleteValidate(t *testing.T) {
	testCases := []struct {
		name         string
		cueValue     string
		isIncomplete bool
		// errors lists the path of each error, followed by whether it is
		// incomplete.
		errors []string
	}{{
		name:         "only incomplete errors",
		cueValue:     `a: int, b: string`,
		isIncomplete: true,
		errors:       []string{"a incomplete", "b incomplete"},
	}, {
		name:         "incomplete and permanent errors",
		cueValue:     `a: int, b: 1 & 2`,
		isIncomplete: false,
		// The incomplete error for "a" should also be reported, but it is
		// dropped in favor of the permanent error.
		errors: []string{"b permanent"},
	}, {
		name:         "error referenced more than once",
		cueValue:     `a: x, b: x, x: 1 & 2`,
		isIncomplete: false,
		errors:       []string{"x permanent"},
	}}

	ctx := cuecontext.New()
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			v := ctx.CompileString(tc.cueValue)
			err := v.Validate(cue.Concrete(true))
			qt.Check(t, qt.Equals(cue.IsIncomplete(err), tc.isIncomplete))

			var got []string
			for _, e := range errors.Errors(err) {
				kind := "permanent"
				if cue.IsIncomplete(e) {
					kind = "incomplete"
				}
				got = append(got, strings.Join(e.Path(), ".")+" "+kind)
			}
			qt.Check(t, qt.DeepEquals(got, tc.errors))
		})
	}
}

func TestIsIncompleteCombined(t *testing.T) {
	ctx := cuecontext.New()
	incomplete := ctx.CompileString(`a: int`).Validate(cue.Concrete(true)).(errors.Error)
	plain := errors.Newf(token.NoPos, "plain error")

	v := ctx.CompileString(`a: int, b: 1 & 2`)
	marshal := func(path string) errors.Error {
		_, err := v.LookupPath(cue.ParsePath(path)).MarshalJSON()
		return err.(errors.Error)
	}
	_, jsonErr := json.Marshal(v.LookupPath(cue.ParsePath("a")))

	testCases := []struct {
		name         string
		err          error
		isIncomplete bool
	}{{
		name:         "incomplete error",
		err:          incomplete,
		isIncomplete: true,
	}, {
		name:         "plain error",
		err:          plain,
		isIncomplete: false,
	}, {
		name:         "non-CUE error",
		err:          fmt.Errorf("plain error"),
		isIncomplete: false,
	}, {
		name: "incomplete and plain errors",
		err:  errors.Append(incomplete, plain),
		// The plain error is permanent, so the combined error should not be
		// incomplete.
		isIncomplete: true,
	}, {
		name: "incomplete and plain errors joined",
		err:  stderrs.Join(incomplete, plain),
		// The plain error is permanent, so the joined error should not be
		// incomplete.
		isIncomplete: true,
	}, {
		name:         "plain and incomplete errors joined",
		err:          stderrs.Join(plain, incomplete),
		isIncomplete: false,
	}, {
		name:         "incomplete error wrapped with fmt.Errorf",
		err:          fmt.Errorf("context: %w", incomplete),
		isIncomplete: true,
	}, {
		name: "incomplete error wrapped with errors.Wrapf",
		err:  errors.Wrapf(incomplete, token.NoPos, "context"),
		// The wrapped error is incomplete, so the wrapping error should be
		// too.
		isIncomplete: false,
	}, {
		name:         "incomplete marshal error",
		err:          marshal("a"),
		isIncomplete: true,
	}, {
		name:         "incomplete marshal error via encoding/json",
		err:          jsonErr,
		isIncomplete: true,
	}, {
		name:         "permanent marshal error",
		err:          marshal("b"),
		isIncomplete: false,
	}, {
		name:         "incomplete and incomplete marshal errors",
		err:          errors.Append(incomplete, marshal("a")),
		isIncomplete: true,
	}, {
		name: "incomplete marshal and plain errors joined",
		err:  stderrs.Join(marshal("a"), plain),
		// The plain error is permanent, so the joined error should not be
		// incomplete.
		isIncomplete: true,
	}}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			qt.Check(t, qt.Equals(cue.IsIncomplete(tc.err), tc.isIncomplete))
		})
	}
}
