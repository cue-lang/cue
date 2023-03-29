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
	"fmt"
	"go/token"
	"reflect"
	"runtime"
	"strings"
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
// - should we derive names from field names? This would require always
//   loading the packages data upon error. Could be an option to disable, or
//   implicitly it would only be loaded if there is an error without message.
// - Option: allow ignore field that lists a set of fields to not be tested
//   for that particular test case: ignore: tdtest.Ignore("want1", "want2")
//

// UpdateTests defines whether tests should be updated by default.
// This can be overridden on an individual basis using T.Update.
var UpdateTests = false

// Config configures a table-driven test.
type Config struct {
	// Update enables updating of Go files.
	Update bool
}

// Set is the set of tests to run.
type Set[TC any] struct {
	t *testing.T

	table []TC
	toRun []int

	updateEnabled bool
	file          string
	info          *info
}

// New creates a test set from a table.
//
// The auto update function requires table to be a direct reference to the
// table.
func New[TC any](t *testing.T, table []TC, cfg *Config) *Set[TC] {
	s := &Set[TC]{
		t:     t,
		table: table,
	}
	if cfg != nil {
		s.updateEnabled = cfg.Update
	} else {
		s.updateEnabled = UpdateTests
	}
	return s
}

// Run runs the given function for each (selected) element in the table.
func Run[TC any](t *testing.T, table []TC, fn func(t *T, tc *TC)) {
	New(t, table, nil).Run(fn)
}

// Run runs the given function for each (selected) element in the table.
func (s *Set[TC]) Run(fn func(t *T, tc *TC)) {
	if len(s.toRun) > 0 {
		for _, i := range s.toRun {
			s.runSingle(i, fn)
		}
	} else {
		for i := range s.table {
			s.runSingle(i, fn)
		}
	}
	if s.info != nil && s.info.needsUpdate {
		s.update()
	}
}

func (s *Set[TC]) runSingle(i int, fn func(t *T, tc *TC)) {
	t := s.t
	name := fmt.Sprint(i)

	x := reflect.ValueOf(s.table[i]).FieldByName("name")
	if x.Kind() == reflect.String {
		name += "/" + x.String()
	}

	t.Run(name, func(t *testing.T) {
		tt := &T{
			T:             t,
			iter:          i,
			infoSrc:       s,
			updateEnabled: s.updateEnabled,
		}
		fn(tt, &s.table[i])
	})
}

// T is a single test case representing an element in a table.
// It embeds *testing.T, so all functions of testing.T are available.
type T struct {
	*testing.T

	infoSrc interface{ getInfo(file string) *info }
	iter    int // position in the table of the current subtest.

	updateEnabled bool
}

func (t *T) info(file string) *info {
	return t.infoSrc.getInfo(file)
}

func (t *T) getCallInfo() (*info, *callInfo) {
	_, file, line, ok := runtime.Caller(2)
	if !ok {
		t.Fatalf("could not update file for test %s", t.Name())
	}
	info := t.info(file)
	return info, info.calls[token.Position{Filename: file, Line: line}]
}

// Equal compares two fields.
//
// For auto updating to work, field must reference a field in the test case
// directly.
func (t *T) Equal(actual, field any, msgAndArgs ...any) {
	t.Helper()

	switch {
	case field == actual:
	case t.updateEnabled:
		info, ci := t.getCallInfo()
		t.updateField(info, ci, actual)
	case len(msgAndArgs) == 0:
		t.Errorf("unexpected value:\ngot:  %v;\nwant: %v", actual, field)
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

// Select species which tests to run. The test may be an int, in which case
// it selects the table entry to run, or a string, which is matched against
// the last path of the test. An empty list runs all tests.
func (t *T) Select(tests ...any) {
	if len(tests) == 0 {
		return
	}

	t.Helper()

	name := t.Name()
	parts := strings.Split(name, "/")

	for _, n := range tests {
		switch n := n.(type) {
		case int:
			if n == t.iter {
				return
			}
		case string:
			if n == parts[len(parts)-1] {
				return
			}
		default:
			panic("unexpected type passed to Select")
		}
	}
	t.Skip("not selected")
}
