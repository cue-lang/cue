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

// Package time provides functionality for representing and displaying time.
//
// The calendrical calculations always assume a Gregorian calendar, with no leap seconds.
package time

// A Time represents an instant in time with nanosecond precision as
// an RFC 3339 string.
//
// A time represented in this format can be marshaled into and from
// a Go time.Time. This means it does not allow the representation of leap
// seconds.
Time: null | =~"^\(date)T\(time)\(nano)\(zone)$"

date = #"\d{4}-(0[1-9]|1[0-2])-(0[1-9]|[1-2]\d|3[0-1])"#
time = #"([0-1]\d|2[0-3]):[0-5]\d:[0-5]\d"#
nano = #"(.\d{1,10})?"# // Go parses up to 10 digits.
zone = #"(Z|(-|\+)\d\d:\d\d)"#

// TODO: correctly constrain days and then leap years/centuries.
