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

package time

import (
	"testing"
)

func TestDuration(t *testing.T) {
	valid := []string{
		"1.0s",
		"1000.0s",
		"1000.000001s",
		".000001s",
		"4h2m",
	}

	for _, tc := range valid {
		t.Run(tc, func(t *testing.T) {
			if b, err := Duration(tc); !b || err != nil {
				t.Errorf("CUE eval failed unexpectedly: %v", err)
			}
		})
	}

	invalid := []string{
		"5d2h",
	}

	for _, tc := range invalid {
		t.Run(tc, func(t *testing.T) {
			if _, err := Duration(tc); err == nil {
				t.Errorf("CUE eval succeeded unexpectedly")
			}
		})
	}
}

func TestParseDuration(t *testing.T) {
	valid := []struct {
		in  string
		out int64
		err bool
	}{
		{"3h2m", 3*Hour + 2*Minute, false},
		{"5s", 5 * Second, false},
		{"5d", 0, true},
	}

	for _, tc := range valid {
		t.Run(tc.in, func(t *testing.T) {
			i, err := ParseDuration(tc.in)
			if got := err != nil; got != tc.err {
				t.Fatalf("error: got %v; want %v", i, tc.out)
			}
			if i != tc.out {
				t.Errorf("got %v; want %v", i, tc.out)
			}
		})
	}
}
