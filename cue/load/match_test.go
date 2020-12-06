// Copyright 2018 The CUE Authors
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

package load

import (
	"reflect"
	"testing"
)

func TestMatch(t *testing.T) {
	c := &Config{}
	what := "default"
	matchFn := func(tag string, want map[string]bool) {
		t.Helper()
		m := make(map[string]bool)
		if !doMatch(c, tag, m) {
			t.Errorf("%s context should match %s, does not", what, tag)
		}
		if !reflect.DeepEqual(m, want) {
			t.Errorf("%s tags = %v, want %v", tag, m, want)
		}
	}
	noMatch := func(tag string, want map[string]bool) {
		m := make(map[string]bool)
		if doMatch(c, tag, m) {
			t.Errorf("%s context should NOT match %s, does", what, tag)
		}
		if !reflect.DeepEqual(m, want) {
			t.Errorf("%s tags = %v, want %v", tag, m, want)
		}
	}

	c.BuildTags = []string{"foo"}
	matchFn("foo", map[string]bool{"foo": true})
	noMatch("!foo", map[string]bool{"foo": true})
	matchFn("foo,!bar", map[string]bool{"foo": true, "bar": true})
	noMatch("!", map[string]bool{})
}
