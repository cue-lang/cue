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
	"strconv"
	"testing"
	"time"
)

func TestTimestamp(t *testing.T) {
	// Valid go times (for JSON marshaling) are represented as is
	validTimes := []string{
		// valid Go times
		"null",
		`"2019-01-02T15:04:05Z"`,
		`"2019-01-02T15:04:05-08:00"`,
		`"2019-01-02T15:04:05.0-08:00"`,
		`"2019-01-02T15:04:05.01-08:00"`,
		`"2019-01-02T15:04:05.012345678-08:00"`,
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

			if tc == "null" {
				return
			}
			str, _ := strconv.Unquote(tc)

			if b, err := Time(str); !b || err != nil {
				t.Errorf("Time failed unexpectedly: %v", err)
			}
			if _, err := Parse(RFC3339Nano, str); err != nil {
				t.Errorf("Parse failed unexpectedly")
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

		// TODO: Go 1.17 rejected the extra digits,
		// and Go 1.18 started accepting them while discarding them.
		// We want CUE to be consistent across Go versions,
		// so we should probably fork Go's time package to behave exactly the
		// way we want and in a consistent way across Go versions.
		// In the meantime, having newer Go versions accept more inputs is not a
		// terrible state of affairs, so for now we disable the test case.
		// `"2019-01-02T15:04:05.01234567890-08:00"`,
	}

	for _, tc := range invalidTimes {
		t.Run(tc, func(t *testing.T) {
			// Test JSON unmarshaling
			var tm time.Time

			if err := json.Unmarshal([]byte(tc), &tm); err == nil {
				t.Errorf("unmarshal JSON succeeded unexpectedly: %v", err)
			}

			str, _ := strconv.Unquote(tc)

			if _, err := Time(str); err == nil {
				t.Errorf("CUE eval succeeded unexpectedly")
			}

			if _, err := Parse(RFC3339Nano, str); err == nil {
				t.Errorf("CUE eval succeeded unexpectedly")
			}
		})
	}
}

func TestUnix(t *testing.T) {
	valid := []struct {
		sec  int64
		nano int64
		want string
	}{
		{0, 0, "1970-01-01T00:00:00Z"},
		{1500000000, 123456, "2017-07-14T02:40:00.000123456Z"},
	}

	for _, tc := range valid {
		t.Run(tc.want, func(t *testing.T) {
			got := Unix(tc.sec, tc.nano)
			if got != tc.want {
				t.Errorf("got %v; want %s", got, tc.want)
			}
		})
	}
}
