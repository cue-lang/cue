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

// This file is maintained by hand: it is the authoritative description
// of the package's API for tooling such as editor completion and hover.
// Its consistency with the registered builtins is checked by the tests
// in cuelang.org/go/pkg.

@experiment(functions)
@pure()

// Package time defines time-related types.
//
// In CUE time values are represented as a string of the format
// time.RFC3339Nano.
package time

Nanosecond: 1

Microsecond: 1000

Millisecond: 1000000

Second: 1000000000

Minute: 60000000000

Hour: 3600000000000

// Duration validates a duration string.
//
// Note: this format also accepts strings of the form '1h3m', '2ms', etc.
// To limit this to seconds only, as often used in JSON, add the !~"hmuµn"
// constraint.
Duration: validator(string) | (func(s: string) -> bool)

// FormatDuration converts nanoseconds to a string representing the duration in
// the form "72h3m0.5s".
//
// Leading zero units are omitted. As a special case, durations less than
// one second use a smaller unit (milli-, micro-, or nanoseconds) to ensure
// that the leading digit is non-zero. The zero duration formats as 0s.
FormatDuration: func(d: int) -> string

// ParseDuration reports the nanoseconds represented by a duration string.
//
// A duration string is a possibly signed sequence of
// decimal numbers, each with optional fraction and a unit suffix,
// such as "300ms", "-1.5h" or "2h45m".
// Valid time units are "ns", "us" (or "µs"), "ms", "s", "m", "h".
ParseDuration: func(s: string) -> int

ANSIC: "Mon Jan _2 15:04:05 2006"

UnixDate: "Mon Jan _2 15:04:05 MST 2006"

RubyDate: "Mon Jan 02 15:04:05 -0700 2006"

RFC822: "02 Jan 06 15:04 MST"

// RFC822 with numeric zone
RFC822Z: "02 Jan 06 15:04 -0700"

RFC850: "Monday, 02-Jan-06 15:04:05 MST"

RFC1123: "Mon, 02 Jan 2006 15:04:05 MST"

// RFC1123 with numeric zone
RFC1123Z: "Mon, 02 Jan 2006 15:04:05 -0700"

RFC3339: "2006-01-02T15:04:05Z07:00"

RFC3339Nano: "2006-01-02T15:04:05.999999999Z07:00"

RFC3339Date: "2006-01-02"

Kitchen: "3:04PM"

Kitchen24: "15:04"

January: 1

February: 2

March: 3

April: 4

May: 5

June: 6

July: 7

August: 8

September: 9

October: 10

November: 11

December: 12

Sunday: 0

Monday: 1

Tuesday: 2

Wednesday: 3

Thursday: 4

Friday: 5

Saturday: 6

// Time validates a RFC3339 date-time.
//
// Caveat: this implementation uses the Go implementation, which does not
// accept leap seconds.
Time: validator(string) | (func(s: string) -> bool)

// Format defines a type string that must adhere to a certain layout.
//
// See Parse for a description on layout strings.
Format: (func(layout: string) -> validator(string)) | (func(value: string, layout: string) -> bool)

// FormatString returns a textual representation of the time value.
// The formatted value is formatted according to the layout defined by the
// argument. See Parse for more information on the layout string.
FormatString: func(layout: string, value: string) -> string

// Parse parses a formatted string and returns the time value it represents.
// The layout defines the format by showing how the reference time,
// defined to be
//
// 	Mon Jan 2 15:04:05 -0700 MST 2006
//
// would be interpreted if it were the value; it serves as an example of
// the input format. The same interpretation will then be made to the
// input string.
//
// Predefined layouts ANSIC, UnixDate, RFC3339 and others describe standard
// and convenient representations of the reference time. For more information
// about the formats and the definition of the reference time, see the
// documentation for ANSIC and the other constants defined by this package.
// Also, the executable example for Time.Format demonstrates the working
// of the layout string in detail and is a good reference.
//
// Elements omitted from the value are assumed to be zero or, when
// zero is impossible, one, so parsing "3:04pm" returns the time
// corresponding to Jan 1, year 0, 15:04:00 UTC (note that because the year is
// 0, this time is before the zero Time).
// Years must be in the range 0000..9999. The day of the week is checked
// for syntax but it is otherwise ignored.
//
// In the absence of a time zone indicator, Parse returns a time in UTC.
//
// A zone offset like -0700 is applied, and the result is converted to UTC.
//
// Parse currently does not support zone abbreviations like MST. All are
// interpreted as UTC.
Parse: func(layout: string, value: string) -> string

// Unix returns the Time, in UTC, corresponding to the given Unix time,
// sec seconds and nsec nanoseconds since January 1, 1970 UTC.
// It is valid to pass nsec outside the range [0, 999999999].
// Not all sec values have a corresponding time value. One such
// value is 1<<63-1 (the largest int64 value).
Unix: func(sec: int, nsec: int) -> string

// ToUnix returns the given time value as a Unix time in seconds
// elapsed since January 1, 1970 UTC.
ToUnix: func(value: string) -> int

// ToUnixNano returns the given time value as a Unix time in nanoseconds
// elapsed since January 1, 1970 UTC.
ToUnixNano: func(value: string) -> int

// Split parses a time string into its individual parts.
Split: func(t: string) -> {...}
