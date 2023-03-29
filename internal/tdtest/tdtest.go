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
	"io"
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

// Set is the set of tests to run.
type Set[TC any] struct {
	t *testing.T

	table []TC
	toRun []int

	info *info
}

// New creates a test set from a table.
//
// The auto update function requires table to be a direct reference to the
// table.
func New[TC any](t *testing.T, table []TC) *Set[TC] {
	return &Set[TC]{
		t:     t,
		table: table,
	}
}

// Update specifies whether tests should be updated in place.
func (s *Set[TC]) Update(doUpdate bool) *Set[TC] {
	switch {
	case doUpdate && s.info != nil:
	case doUpdate:
		s.info = s.getInfo()
	default:
		s.info = nil
	}
	return s
}

// Select species which tests to run. It overrides any previous calls to
// Select. If no entries are given it means all tests should be run.
// Select filters independently of Match.
func (s *Set[TC]) Select(nr ...int) *Set[TC] {
	s.toRun = nr
	return s
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

	x := reflect.Indirect(reflect.ValueOf(s.table[i])).FieldByName("name")
	if x.Kind() == reflect.String {
		name += "/" + x.String()
	}

	t.Run(name, func(t *testing.T) {
		tt := &T{
			T:    t,
			iter: i,
			info: s.info,
		}
		fn(tt, &s.table[i])

		for _, w := range tt.writers {
			_ = w
		}
	})
}

// T is a single test case representing an element in a table.
// It embeds *testing.T, so all functions of testing.T are available.
type T struct {
	*testing.T

	iter    int // position in the table of the current subtest.
	info    *info
	writers []writer // used by EqualWriter
}

type writer struct {
	w      *strings.Builder
	ci     *callInfo
	field  string
	format string
	args   []any
}

// Equal compares two fields.
//
// For auto updating to work, field must reference the field directly.
func (t *T) Equal(field, actual any, msgAndArgs ...any) {
	t.Helper()

	switch {
	case field == actual:
	case t.info != nil:
		_, file, line, _ := runtime.Caller(1)
		ci := t.getCallInfo(file, line)
		t.updateField(ci, actual)
	case len(msgAndArgs) == 0:
		t.Errorf("unexpected value:\ngot:  %v;\nwant: %v", actual, field)
	default:
		format := msgAndArgs[0].(string) + ":\ngot:  %v;\nwant: %v"
		args := append(msgAndArgs[1:], actual, field)
		t.Errorf(format, args...)
	}
}

func (t *T) finalizeWriter(w *writer) {
	actual := w.w.String()
	switch {
	case w.field == actual:
	case t.info != nil:
		t.updateField(w.ci, actual)
	case len(w.format) == 0:
		t.Errorf("unexpected value:\ngot:  %v;\nwant: %v", actual, w.field)
	default:
		t.Errorf(w.format, w.args...)
	}
}

// EqualWriter returns a writer the contents of which are checked against
// field upon the return of the Run function in which it is called.
// field must be of type string.
func (t *T) EqualWriter(field string, msgAndArgs ...any) io.Writer {
	t.Helper()

	w := writer{
		w:     &strings.Builder{},
		field: field,
	}
	if t.info != nil {
		_, file, line, _ := runtime.Caller(1)
		w.ci = t.getCallInfo(file, line)
	}
	if len(msgAndArgs) > 0 {
		w.format = msgAndArgs[0].(string)
		w.args = msgAndArgs[1:]
	}
	t.writers = append(t.writers, w)

	return w.w
}
