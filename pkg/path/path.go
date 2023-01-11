// Copyright 2020 CUE Authors
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

// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package filepath implements utility routines for manipulating filename paths
// in a way compatible with the target operating system-defined file paths.
//
// The filepath package uses either forward slashes or backslashes,
// depending on the operating system. To process paths such as URLs
// that always use forward slashes regardless of the operating
// system, see the path package.
package path

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
func Clean(path string, os OS) string {
	return getOS(os).Clean(path)
}

// ToSlash returns the result of replacing each separator character
// in path with a slash ('/') character. Multiple separators are
// replaced by multiple slashes.
func ToSlash(path string, os OS) string {
	return getOS(os).ToSlash(path)
}

// FromSlash returns the result of replacing each slash ('/') character
// in path with a separator character. Multiple slashes are replaced
// by multiple separators.
func FromSlash(path string, os OS) string {
	return getOS(os).FromSlash(path)
}

// SplitList splits a list of paths joined by the OS-specific ListSeparator,
// usually found in PATH or GOPATH environment variables.
// Unlike strings.Split, SplitList returns an empty slice when passed an empty
// string.
func SplitList(path string, os OS) []string {
	return getOS(os).SplitList(path)
}

// Split splits path immediately following the final slash and returns them as
// the list [dir, file], separating it into a directory and file name component.
// If there is no slash in path, Split returns an empty dir and file set to
// path. The returned values have the property that path = dir+file.
// The default value for os is Unix.
func Split(path string, os OS) []string {
	return getOS(os).Split(path)
}

// Join joins any number of path elements into a single path,
// separating them with an OS specific Separator. Empty elements
// are ignored. The result is Cleaned. However, if the argument
// list is empty or all its elements are empty, Join returns
// an empty string.
// On Windows, the result will only be a UNC path if the first
// non-empty element is a UNC path.
// The default value for os is Unix.
func Join(elem []string, os OS) string {
	return getOS(os).Join(elem)
}

// Ext returns the file name extension used by path.
// The extension is the suffix beginning at the final dot
// in the final element of path; it is empty if there is
// no dot. The default value for os is Unix.
func Ext(path string, os OS) string {
	return getOS(os).Ext(path)
}

// Resolve reports the path of sub relative to dir. If sub is an absolute path,
// or if dir is empty, it will return sub. If sub is empty, it will return dir.
// Resolve calls Clean on the result. The default value for os is Unix.
func Resolve(dir, sub string, os OS) string {
	return getOS(os).Resolve(dir, sub)
}

// Rel returns a relative path that is lexically equivalent to targpath when
// joined to basepath with an intervening separator. That is,
// Join(basepath, Rel(basepath, targpath)) is equivalent to targpath itself.
// On success, the returned path will always be relative to basepath,
// even if basepath and targpath share no elements.
// An error is returned if targpath can't be made relative to basepath or if
// knowing the current working directory would be necessary to compute it.
// Rel calls Clean on the result. The default value for os is Unix.
func Rel(basepath, targpath string, os OS) (string, error) {
	return getOS(os).Rel(basepath, targpath)
}

// Base returns the last element of path.
// Trailing path separators are removed before extracting the last element.
// If the path is empty, Base returns ".".
// If the path consists entirely of separators, Base returns a single separator.
// The default value for os is Unix.
func Base(path string, os OS) string {
	return getOS(os).Base(path)
}

// Dir returns all but the last element of path, typically the path's directory.
// After dropping the final element, Dir calls Clean on the path and trailing
// slashes are removed.
// If the path is empty, Dir returns ".".
// If the path consists entirely of separators, Dir returns a single separator.
// The returned path does not end in a separator unless it is the root directory.
// The default value for os is Unix.
func Dir(path string, os OS) string {
	return getOS(os).Dir(path)
}

// IsAbs reports whether the path is absolute. The default value for os is Unix.
// Note that because IsAbs has a default value, it cannot be used as
// a validator.
func IsAbs(path string, os OS) bool {
	return getOS(os).IsAbs(path)
}

// VolumeName returns leading volume name.
// Given "C:\foo\bar" it returns "C:" on Windows.
// Given "\\host\share\foo" it returns "\\host\share".
// On other platforms it returns "".
// The default value for os is Windows.
func VolumeName(path string, os OS) string {
	return getOS(os).VolumeName(path)
}
