// Copyright 2023 CUE Authors
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

// Package tdtest provides support for table-driven testing.
//
// Features include automatically updating of test values, automatic error
// message generation, and singling out single tests to run.
//
// Auto updating fields is only supported for fields that are scalar types:
// string, bool, int*, and uint*. If the field is a string, the "actual" value
// may be any Go value that can meaningfully be printed with fmt.Sprint.
package tdtest

import (
	"errors"
	"fmt"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

// TODO:
// - make this a public package at some point.
// - add tests. Maybe adding Examples is sufficient.
// - use text-based modification, instead of astutil. The latter is too brittle.
// - allow updating position-based, instead of named, fields.
// - implement skip, maybe match
// - make name field explicit, i.e. Name("name"), field tag, or tdtest.Name type.
// - allow "skip" field. Again either SkipName("skip"), tag, or Skip type.
// - allow for tdtest:"noupdate" field tag.
// - Option: allow ignore field that lists a set of fields to not be tested
//   for that particular test case: ignore: tdtest.Ignore("want1", "want2")
//

// UpdateTests reports whether tests should be updated by default.
// This can be overridden on an individual basis using T.Update.
// It is a function so that the value is computed while a test runs;
// see the comment on Init in internal/cueexperiment for why that
// matters to the go test cache.
var UpdateTests = func() bool { return false }

// set is the set of tests to run.
type set[TC any] struct {
	t *testing.T

	table []TC

	updateEnabled bool
	info          *info
}

// Run runs the given function for each (selected) element in the table.
// TC must be a struct type. If that has a string field named "name",
// that value will be used to name the associated subtest.
func Run[TC any](t *testing.T, table []TC, fn func(t *T, tc *TC)) {
	s := &set[TC]{
		t:             t,
		table:         table,
		updateEnabled: UpdateTests(),
	}
	for i := range s.table {
		name := fmt.Sprint(i)

		x := reflect.ValueOf(s.table[i]).FieldByName("name")
		if x.Kind() == reflect.String {
			name += "/" + x.String()
		}

		s.t.Run(name, func(t *testing.T) {
			tt := &T{
				T:             t,
				iter:          i,
				infoSrc:       s,
				updateEnabled: s.updateEnabled,
			}
			fn(tt, &s.table[i])
		})
	}
	if s.info != nil && s.info.needsUpdate {
		s.update()
	}
}

// T is a single test case representing an element in a table.
// It embeds *testing.T, so all functions of testing.T are available.
type T struct {
	*testing.T

	infoSrc interface{ getInfo(file string) *info }
	iter    int // position in the table of the current subtest.

	updateEnabled bool
}

// getCallInfo returns the information needed to update the field compared by
// the calling Equal, or an error saying why the field cannot be updated.
func (t *T) getCallInfo() (*info, string, error) {
	_, file, line, ok := runtime.Caller(2)
	if !ok {
		t.Fatalf("could not update file for test %s", t.Name())
	}
	// Note: it seems that sometimes the file returned by Caller
	// might not be in canonical format (under Windows, it can contain
	// forward slashes), so clean it.
	file = filepath.Clean(file)
	info := t.infoSrc.getInfo(file)
	if info.err != nil {
		return nil, "", info.err
	}
	fieldName, ok := info.calls[token.Position{Filename: file, Line: line}]
	if !ok {
		return nil, "", errors.New("Equal call is outside the function passed to Run")
	}
	return info, fieldName, nil
}

// Equal compares two fields.
//
// For auto updating to work, field must reference a field in the test case
// directly, and the call must be within the function literal which the test
// function passes to Run, rather than in a helper.
func (t *T) Equal(actual, field any, msgAndArgs ...any) {
	t.Helper()

	switch {
	case field == actual:
	case t.updateEnabled:
		info, fieldName, err := t.getCallInfo()
		if err != nil {
			t.Fatalf("cannot update a field: %v:\ngot:  %v;\nwant: %v", err, actual, field)
		}
		t.updateField(info, fieldName, actual)
	case len(msgAndArgs) == 0:
		if _, fieldName, err := t.getCallInfo(); err == nil {
			t.Errorf("unexpected value for field %s:\ngot:  %v;\nwant: %v", fieldName, actual, field)
		} else {
			t.Errorf("unexpected value:\ngot:  %v;\nwant: %v", actual, field)
		}
	default:
		format := msgAndArgs[0].(string) + ":\ngot:  %v;\nwant: %v"
		args := append(msgAndArgs[1:], actual, field)
		t.Errorf(format, args...)
	}
}

// Update specifies whether to update the Go structs in case of discrepancies.
// It overrides the default setting.
func (t *T) Update(enable bool) {
	t.updateEnabled = enable
}
