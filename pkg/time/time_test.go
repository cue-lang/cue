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
	"encoding/json"
	"testing"
	"time"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/cue/parser"
)

func TestTime(t *testing.T) {
	inst := cue.Build(load.Instances([]string{"."}, nil))[0]
	if inst.Err != nil {
		t.Fatal(inst.Err)
	}

	parseCUE := func(t *testing.T, time string) error {
		expr, err := parser.ParseExpr("test", "Time&"+time)
		if err != nil {
			t.Fatal(err)
		}
		v := inst.Eval(expr)
		return v.Err()
	}

	// Valid go times (for JSON marshaling) are represented as is
	validTimes := []string{
		// valid Go times
		"null",
		`"2019-01-02T15:04:05Z"`,
		`"2019-01-02T15:04:05-08:00"`,
		`"2019-01-02T15:04:05.0-08:00"`,
		`"2019-01-02T15:04:05.01-08:00"`,
		`"2019-01-02T15:04:05.0123456789-08:00"`, // Is this a Go bug?
		`"2019-02-28T15:04:59Z"`,

		// TODO: allow leap seconds? This is allowed by the RFC 3339 spec.
		// `"2019-06-30T23:59:60Z"`, // leap seconds
	}

	for _, tc := range validTimes {
		t.Run(tc, func(t *testing.T) {
			// Test JSON unmarshaling
			var tm time.Time

			if err := json.Unmarshal([]byte(tc), &tm); err != nil {
				t.Errorf("unmarshal JSON failed unexpectedly: %v", err)
			}

			if err := parseCUE(t, tc); err != nil {
				t.Errorf("CUE eval failed unexpectedly: %v", err)
			}
		})
	}

	invalidTimes := []string{
		`"2019-01-02T15:04:05"`,        // missing time zone
		`"2019-01-02T15:04:61Z"`,       // seconds out of range
		`"2019-01-02T15:60:00Z"`,       // minute out of range
		`"2019-01-02T24:00:00Z"`,       // hour out of range
		`"2019-01-32T23:00:00Z"`,       // day out of range
		`"2019-01-00T23:00:00Z"`,       // day out of range
		`"2019-00-15T23:00:00Z"`,       // month out of range
		`"2019-13-15T23:00:00Z"`,       // month out of range
		`"2019-01-02T15:04:05Z+08:00"`, // double time zone
		`"2019-01-02T15:04:05+08"`,     // partial time zone
		`"2019-01-02T15:04:05.01234567890-08:00"`,
	}

	for _, tc := range invalidTimes {
		t.Run(tc, func(t *testing.T) {
			// Test JSON unmarshaling
			var tm time.Time

			if err := json.Unmarshal([]byte(tc), &tm); err == nil {
				t.Errorf("unmarshal JSON succeeded unexpectedly: %v", err)
			}

			if err := parseCUE(t, tc); err == nil {
				t.Errorf("CUE eval succeeded unexpectedly: %v", err)
			}
		})
	}
}
