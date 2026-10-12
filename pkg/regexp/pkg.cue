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

// Package regexp implements regular expression search.
//
// The syntax of the regular expressions accepted is the same
// general syntax used by Perl, Python, and other languages.
// More precisely, it is the syntax accepted by RE2 and described at
// https://golang.org/s/re2syntax, except for \C.
// For an overview of the syntax, run
//
// 	go doc regexp/syntax
//
// The regexp implementation provided by this package is
// guaranteed to run in time linear in the size of the input.
// (This is a property not guaranteed by most open source
// implementations of regular expressions.) For more information
// about this property, see
//
// 	https://swtch.com/~rsc/regexp/regexp1.html
//
// or any book about automata theory.
//
// All characters are UTF-8-encoded code points.
//
// The regexp package functions match a regular expression and identify
// the matched text. Their names are matched by this regular expression:
//
// 	Find(All)?(Submatch)?
//
// If 'All' is present, the routine matches successive non-overlapping
// matches of the entire expression. Empty matches abutting a preceding
// match are ignored. The return value is a slice containing the successive
// return values of the corresponding non-'All' routine. These routines take
// an extra integer argument, n. If n >= 0, the function returns at most n
// matches/submatches; otherwise, it returns all of them.
//
// If 'Submatch' is present, the return value is a slice identifying the
// successive submatches of the expression. Submatches are matches of
// parenthesized subexpressions (also known as capturing groups) within the
// regular expression, numbered from left to right in order of opening
// parenthesis. Submatch 0 is the match of the entire expression, submatch 1
// the match of the first parenthesized subexpression, and so on.
package regexp

// Find returns a string holding the text of the leftmost match in s of the regular expression.
// A return value of bottom indicates no match.
Find: func(pattern: string, s: string) -> string

// FindAll is the 'All' version of Find; it returns a list of all successive
// matches of the expression, as defined by the 'All' description in the
// package comment.
// A return value of bottom indicates no match.
FindAll: func(pattern: string, s: string, n: int) -> [...string]

// FindAllNamedSubmatch is like [FindAllSubmatch], but returns a list of maps
// with the names used in capturing groups. See [FindNamedSubmatch] for an
// example on how to use named groups.
FindAllNamedSubmatch: func(pattern: string, s: string, n: int) -> [...]

// FindAllSubmatch is the 'All' version of [FindSubmatch]; it returns a list
// of all successive matches of the expression, as defined by the 'All'
// description in the package comment.
// A return value of bottom indicates no match.
FindAllSubmatch: func(pattern: string, s: string, n: int) -> [...]

// FindNamedSubmatch is like [FindSubmatch], but returns a map with the names used
// in capturing groups. Unnamed groups are omitted, so a pattern without
// named groups results in an empty map.
//
// Example:
//
// 	regexp.FindNamedSubmatch(#"Hello (?P<person>\w*)!"#, "Hello World!")
//
// Output:
//
// 	{person: "World"}
FindNamedSubmatch: func(pattern: string, s: string) -> {...}

// FindSubmatch returns a list holding the text of the leftmost
// match of the regular expression in s and the matches, if any, of its
// subexpressions, as defined by the 'Submatch' descriptions in the package
// comment.
// A return value of bottom indicates no match.
FindSubmatch: func(pattern: string, s: string) -> [...string]

// ReplaceAll returns a copy of src, replacing variables in repl with
// corresponding matches drawn from src, according to the following rules.
//
// In the template repl, a variable is denoted by a substring of the form $name
// or ${name}, where name is a non-empty sequence of letters, digits, and
// underscores. A purely numeric name like $1 refers to the submatch with the
// corresponding index; other names refer to capturing parentheses named with
// the (?P<name>...) syntax. A reference to an out of range or unmatched index
// or a name that is not present in the regular expression is replaced with an
// empty string.
//
// In the $name form, name is taken to be as long as possible: $1x is
// equivalent to ${1x}, not ${1}x, and, $10 is equivalent to ${10}, not ${1}0.
//
// To insert a literal $ in the output, use $$ in the template.
ReplaceAll: func(pattern: string, src: string, repl: string) -> string

// ReplaceAllLiteral returns a copy of src, replacing matches of the regexp
// pattern with the replacement string repl. The replacement repl is substituted
// directly.
ReplaceAllLiteral: func(pattern: string, src: string, repl: string) -> string

// Valid reports whether the given regular expression
// is valid.
Valid: validator(string) | (func(pattern: string) -> bool)

// Match reports whether the string s
// contains any match of the regular expression pattern.
Match: func(pattern: string, s: string) -> bool

// QuoteMeta returns a string that escapes all regular expression metacharacters
// inside the argument text; the returned string is a regular expression matching
// the literal text.
QuoteMeta: func(s: string) -> string
