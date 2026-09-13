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

// Package path implements utility routines for manipulating filename paths as
// defined by targetted operating systems, and also paths that always use
// forward slashes regardless of the operating system, such as URLs.
package path

// Match reports whether name matches the shell file name pattern.
// The pattern syntax is:
//
// 	pattern:
// 		{ term }
// 	term:
// 		'*'         matches any sequence of non-Separator characters
// 		'?'         matches any single non-Separator character
// 		'[' [ '^' ] { character-range } ']'
// 		            character class (must be non-empty)
// 		c           matches character c (c != '*', '?', '\\', '[')
// 		'\\' c      matches character c
//
// 	character-range:
// 		c           matches character c (c != '\\', '-', ']')
// 		'\\' c      matches character c
// 		lo '-' hi   matches character c for lo <= c <= hi
//
// Match requires pattern to match all of name, not just a substring.
// The only possible returned error is ErrBadPattern, when pattern
// is malformed.
//
// On Windows, escaping is disabled. Instead, '\\' is treated as
// path separator.
//
// A pattern may not contain '**', as a wildcard matching separator characters
// is not supported at this time.
Match: func(pattern: string, name: string, os: string = "unix") -> bool

Unix: "unix"

Windows: "windows"

Plan9: "plan9"

// Clean returns the shortest path name equivalent to path
// by purely lexical processing. The default value for os is Unix.
// It applies the following rules
// iteratively until no further processing can be done:
//
//  1. Replace multiple Separator elements with a single one.
//  2. Eliminate each . path name element (the current directory).
//  3. Eliminate each inner .. path name element (the parent directory)
//     along with the non-.. element that precedes it.
//  4. Eliminate .. elements that begin a rooted path:
//     that is, replace "/.." by "/" at the beginning of a path,
//     assuming Separator is '/'.
//
// The returned path ends in a slash only if it represents a root directory,
// such as "/" on Unix or `C:\` on Windows.
//
// Finally, any occurrences of slash are replaced by Separator.
//
// If the result of this process is an empty string, Clean
// returns the string ".".
//
// See also Rob Pike, “Lexical File Names in Plan 9 or
// Getting Dot-Dot Right,”
// https://9p.io/sys/doc/lexnames.html
Clean: func(path: string, os: string = "unix") -> string

// ToSlash returns the result of replacing each separator character
// in path with a slash ('/') character. Multiple separators are
// replaced by multiple slashes.
ToSlash: func(path: string, os: string) -> string

// FromSlash returns the result of replacing each slash ('/') character
// in path with a separator character. Multiple slashes are replaced
// by multiple separators.
FromSlash: func(path: string, os: string) -> string

// SplitList splits a list of paths joined by the OS-specific ListSeparator,
// usually found in PATH or GOPATH environment variables.
// Unlike strings.Split, SplitList returns an empty slice when passed an empty
// string.
SplitList: func(path: string, os: string) -> [...string]

// Split splits path immediately following the final slash and returns them as
// the list [dir, file], separating it into a directory and file name component.
// If there is no slash in path, Split returns an empty dir and file set to
// path. The returned values have the property that path = dir+file.
// The default value for os is Unix.
Split: func(path: string, os: string = "unix") -> [...string]

// Join joins any number of path elements into a single path,
// separating them with an OS specific Separator. Empty elements
// are ignored. The result is Cleaned. However, if the argument
// list is empty or all its elements are empty, Join returns
// an empty string.
// On Windows, the result will only be a UNC path if the first
// non-empty element is a UNC path.
// The default value for os is Unix.
Join: func(elem: [...string], os: string = "unix") -> string

// Ext returns the file name extension used by path.
// The extension is the suffix beginning at the final dot
// in the final element of path; it is empty if there is
// no dot. The default value for os is Unix.
Ext: func(path: string, os: string = "unix") -> string

// Resolve reports the path of sub relative to dir. If sub is an absolute path,
// or if dir is empty, it will return sub. If sub is empty, it will return dir.
// Resolve calls Clean on the result. The default value for os is Unix.
Resolve: func(dir: string, sub: string, os: string = "unix") -> string

// Rel returns a relative path that is lexically equivalent to targpath when
// joined to basepath with an intervening separator. That is,
// Join(basepath, Rel(basepath, targpath)) is equivalent to targpath itself.
// On success, the returned path will always be relative to basepath,
// even if basepath and targpath share no elements.
// An error is returned if targpath can't be made relative to basepath or if
// knowing the current working directory would be necessary to compute it.
// Rel calls Clean on the result. The default value for os is Unix.
Rel: func(basepath: string, targpath: string, os: string = "unix") -> string

// Base returns the last element of path.
// Trailing path separators are removed before extracting the last element.
// If the path is empty, Base returns ".".
// If the path consists entirely of separators, Base returns a single separator.
// The default value for os is Unix.
Base: func(path: string, os: string = "unix") -> string

// Dir returns all but the last element of path, typically the path's directory.
// After dropping the final element, Dir calls Clean on the path and trailing
// slashes are removed.
// If the path is empty, Dir returns ".".
// If the path consists entirely of separators, Dir returns a single separator.
// The returned path does not end in a separator unless it is the root directory.
// The default value for os is Unix.
Dir: func(path: string, os: string = "unix") -> string

// IsAbs reports whether the path is absolute. The default value for os is Unix.
// Note that because IsAbs has a default value, it cannot be used as
// a validator.
IsAbs: func(path: string, os: string = "unix") -> bool

// VolumeName returns leading volume name.
// Given "C:\foo\bar" it returns "C:" on Windows.
// Given "\\host\share\foo" it returns "\\host\share".
// On other platforms it returns "".
// The default value for os is Windows.
VolumeName: func(path: string, os: string = "unix") -> string
