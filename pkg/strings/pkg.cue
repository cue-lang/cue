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

// Package strings implements simple functions to manipulate UTF-8 encoded
// strings.
//
// Some of the functions in this package are specifically intended as
// validators. For instance, MaxRunes as used in this CUE program
//
// 	import "strings"
//
// 	myString: strings.MaxRunes(5)
//
// specifies that the myString should be at most 5 code points.
package strings

// ByteAt reports the ith byte of the underlying byte slice.
ByteAt: func(b: bytes | string, i: int) -> int

// ByteSlice reports the bytes of the underlying byte slice from the start
// index up to but not including the end index.
ByteSlice: func(b: bytes | string, start: int, end: int) -> bytes

// Runes returns the Unicode code points of the given string.
Runes: func(s: string) -> [...]

// Repeat returns a new string consisting of count copies of the string s.
Repeat: func(s: string, count: int) -> string

// MinRunes reports whether the number of runes (Unicode codepoints) in a string
// is at least a certain minimum. MinRunes can be used as a validator to
// accept all strings for which this property holds.
MinRunes: (func(min: int) -> validator(string)) | (func(s: string, min: int) -> bool)

// MaxRunes reports whether the number of runes (Unicode codepoints) in a string
// exceeds a certain maximum. MaxRunes can be used as a validator to
// accept all strings for which this property holds.
MaxRunes: (func(max: int) -> validator(string)) | (func(s: string, max: int) -> bool)

// ToTitle returns a copy of the string s with all Unicode letters that begin
// words mapped to their title case.
ToTitle: func(s: string) -> string

// ToCamel returns a copy of the string s with all Unicode letters that begin
// words mapped to lower case.
ToCamel: func(s: string) -> string

// SliceRunes returns a string of the underlying string data from the start index
// up to but not including the end index.
SliceRunes: func(s: string, start: int, end: int) -> string

// Compare returns an integer comparing two strings lexicographically.
// The result will be 0 if a==b, -1 if a < b, and +1 if a > b.
//
// Compare is included only for symmetry with package bytes.
// It is usually clearer and always faster to use the built-in
// string comparison operators ==, <, >, and so on.
Compare: func(a: string, b: string) -> int

// Count counts the number of non-overlapping instances of substr in s.
// If substr is an empty string, Count returns 1 + the number of Unicode code points in s.
Count: func(s: string, substr: string) -> int

// Contains reports whether substr is within s.
Contains: func(s: string, substr: string) -> bool

// ContainsAny reports whether any Unicode code points in chars are within s.
ContainsAny: func(s: string, chars: string) -> bool

// LastIndex returns the index of the last instance of substr in s, or -1 if substr is not present in s.
LastIndex: func(s: string, substr: string) -> int

// IndexAny returns the index of the first instance of any Unicode code point
// from chars in s, or -1 if no Unicode code point from chars is present in s.
IndexAny: func(s: string, chars: string) -> int

// LastIndexAny returns the index of the last instance of any Unicode code
// point from chars in s, or -1 if no Unicode code point from chars is
// present in s.
LastIndexAny: func(s: string, chars: string) -> int

// SplitN slices s into substrings separated by sep and returns a slice of
// the substrings between those separators.
//
// The count determines the number of substrings to return:
//
// 	n > 0: at most n substrings; the last substring will be the unsplit remainder.
// 	n == 0: the result is nil (zero substrings)
// 	n < 0: all substrings
//
// Edge cases for s and sep (for example, empty strings) are handled
// as described in the documentation for Split.
SplitN: func(s: string, sep: string, n: int) -> [...string]

// SplitAfterN slices s into substrings after each instance of sep and
// returns a slice of those substrings.
//
// The count determines the number of substrings to return:
//
// 	n > 0: at most n substrings; the last substring will be the unsplit remainder.
// 	n == 0: the result is nil (zero substrings)
// 	n < 0: all substrings
//
// Edge cases for s and sep (for example, empty strings) are handled
// as described in the documentation for SplitAfter.
SplitAfterN: func(s: string, sep: string, n: int) -> [...string]

// Split slices s into all substrings separated by sep and returns a slice of
// the substrings between those separators.
//
// If s does not contain sep and sep is not empty, Split returns a
// slice of length 1 whose only element is s.
//
// If sep is empty, Split splits after each UTF-8 sequence. If both s
// and sep are empty, Split returns an empty slice.
//
// It is equivalent to SplitN with a count of -1.
Split: func(s: string, sep: string) -> [...string]

// SplitAfter slices s into all substrings after each instance of sep and
// returns a slice of those substrings.
//
// If s does not contain sep and sep is not empty, SplitAfter returns
// a slice of length 1 whose only element is s.
//
// If sep is empty, SplitAfter splits after each UTF-8 sequence. If
// both s and sep are empty, SplitAfter returns an empty slice.
//
// It is equivalent to SplitAfterN with a count of -1.
SplitAfter: func(s: string, sep: string) -> [...string]

// Fields splits the string s around each instance of one or more consecutive white space
// characters, as defined by unicode.IsSpace, returning a slice of substrings of s or an
// empty slice if s contains only white space.
Fields: func(s: string) -> [...string]

// Join concatenates the elements of its first argument to create a single string. The separator
// string sep is placed between elements in the resulting string.
Join: func(elems: [...string], sep: string) -> string

// HasPrefix tests whether the string s begins with prefix.
HasPrefix: func(s: string, prefix: string) -> bool

// HasSuffix tests whether the string s ends with suffix.
HasSuffix: func(s: string, suffix: string) -> bool

// ToUpper returns s with all Unicode letters mapped to their upper case.
ToUpper: func(s: string) -> string

// ToLower returns s with all Unicode letters mapped to their lower case.
ToLower: func(s: string) -> string

// Trim returns a slice of the string s with all leading and
// trailing Unicode code points contained in cutset removed.
Trim: func(s: string, cutset: string) -> string

// TrimLeft returns a slice of the string s with all leading
// Unicode code points contained in cutset removed.
//
// To remove a prefix, use [TrimPrefix] instead.
TrimLeft: func(s: string, cutset: string) -> string

// TrimRight returns a slice of the string s, with all trailing
// Unicode code points contained in cutset removed.
//
// To remove a suffix, use [TrimSuffix] instead.
TrimRight: func(s: string, cutset: string) -> string

// TrimSpace returns a slice of the string s, with all leading
// and trailing white space removed, as defined by Unicode.
TrimSpace: func(s: string) -> string

// TrimPrefix returns s without the provided leading prefix string.
// If s doesn't start with prefix, s is returned unchanged.
TrimPrefix: func(s: string, prefix: string) -> string

// TrimSuffix returns s without the provided trailing suffix string.
// If s doesn't end with suffix, s is returned unchanged.
TrimSuffix: func(s: string, suffix: string) -> string

// Replace returns a copy of the string s with the first n
// non-overlapping instances of old replaced by new.
// If old is empty, it matches at the beginning of the string
// and after each UTF-8 sequence, yielding up to k+1 replacements
// for a k-rune string.
// If n < 0, there is no limit on the number of replacements.
Replace: func(s: string, old: string, new: string, n: int) -> string

// Index returns the index of the first instance of substr in s, or -1 if substr is not present in s.
Index: func(s: string, substr: string) -> int
