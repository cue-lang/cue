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

// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package path

import (
	"strings"
)

type windowsInfo struct{}

var _ osInfo = windowsInfo{}

const (
	windowsSeparator     = '\\'
	windowsListSeparator = ';'
)

func isSlash(c uint8) bool {
	return c == '\\' || c == '/'
}

func (os windowsInfo) IsPathSeparator(b byte) bool {
	return isSlash(b)
}

// IsAbs reports whether the path is absolute.
func (os windowsInfo) IsAbs(path string) (b bool) {
	l := os.volumeNameLen(path)
	if l == 0 {
		return false
	}
	// If the volume name starts with a double slash, this is an absolute path.
	if isSlash(path[0]) && isSlash(path[1]) {
		return true
	}
	path = path[l:]
	if path == "" {
		return false
	}
	return isSlash(path[0])
}

// volumeNameLen returns length of the leading volume name on Windows.
// It returns 0 elsewhere.
//
// See:
// https://learn.microsoft.com/en-us/dotnet/standard/io/file-path-formats
// https://googleprojectzero.blogspot.com/2016/02/the-definitive-guide-on-win32-to-nt.html
func (os windowsInfo) volumeNameLen(path string) int {
	switch {
	case len(path) >= 2 && path[1] == ':':
		// Path starts with a drive letter.
		//
		// Not all Windows functions necessarily enforce the requirement that
		// drive letters be in the set A-Z, and we don't try to here.
		//
		// We don't handle the case of a path starting with a non-ASCII character,
		// in which case the "drive letter" might be multiple bytes long.
		return 2

	case len(path) == 0 || !isSlash(path[0]):
		// Path does not have a volume component.
		return 0

	case pathHasPrefixFold(path, `\\.`) ||
		pathHasPrefixFold(path, `\\?`) || pathHasPrefixFold(path, `\??`):
		// Path starts with a device prefix: \\.\ for Local Device paths,
		// or \\?\ or \??\ for Root Local Device paths.
		switch {
		case len(path) == 3:
			return 3 // exactly \\., \\?, or \??
		case pathHasPrefixFold(path[4:], `UNC`):
			// We're going to treat the UNC host and share as part of the volume
			// prefix for historical reasons, but this isn't really principled;
			// Windows's own GetFullPathName will happily remove the first
			// component of the path in this space, converting
			// \\.\unc\a\b\..\c into \\.\unc\a\c.
			return validVolumeNameLen(path, uncLen(path, len(`\\.\UNC\`)))
		case pathHasPrefixFold(path[4:], `GLOBALROOT`):
			// GLOBALROOT links to the root of the NT object namespace. A volume
			// device path such as \\?\GLOBALROOT\Device\HarddiskVolume1\ has two
			// components after GLOBALROOT. Treat them as the volume name so Clean
			// preserves the separator denoting the volume root. This is a lexical
			// approximation; other object namespace paths can have different layouts.
			return validVolumeNameLen(path, uncLen(path, len(`\\?\GLOBALROOT\`)))
		}
		// We treat the next component after the device prefix as
		// part of the volume name, which means Clean(`\\?\c:\`)
		// won't remove the trailing \.
		_, rest, ok := cutPath(path[4:])
		if !ok {
			return validVolumeNameLen(path, len(path))
		}
		return validVolumeNameLen(path, len(path)-len(rest)-1)

	case len(path) >= 2 && isSlash(path[1]):
		// Path starts with \\, and is a UNC path.
		return validVolumeNameLen(path, uncLen(path, 2))
	}
	return 0
}

// validVolumeNameLen returns n if path[:n] is a valid Windows volume name.
// If the volume name contains a ".." path component, it returns 0.
func validVolumeNameLen(path string, n int) int {
	for p := path[:n]; p != ""; {
		var part string
		part, p, _ = cutPath(p)
		if part == ".." {
			return 0
		}
	}
	return n
}

// pathHasPrefixFold tests whether the path s begins with prefix,
// ignoring case and treating all path separators as equivalent.
// If s is longer than prefix, then s[len(prefix)] must be a path separator.
func pathHasPrefixFold(s, prefix string) bool {
	if len(s) < len(prefix) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		if isSlash(prefix[i]) {
			if !isSlash(s[i]) {
				return false
			}
		} else if toUpper(prefix[i]) != toUpper(s[i]) {
			return false
		}
	}
	if len(s) > len(prefix) && !isSlash(s[len(prefix)]) {
		return false
	}
	return true
}

// uncLen returns the length of the volume prefix of a UNC path.
// prefixLen is the prefix prior to the start of the UNC host;
// for example, for "//host/share", the prefixLen is len("//")==2.
func uncLen(path string, prefixLen int) int {
	count := 0
	for i := prefixLen; i < len(path); i++ {
		if isSlash(path[i]) {
			count++
			if count == 2 {
				return i
			}
		}
	}
	return len(path)
}

// cutPath slices path around the first path separator.
func cutPath(path string) (before, after string, found bool) {
	for i := range path {
		if isSlash(path[i]) {
			return path[:i], path[i+1:], true
		}
	}
	return path, "", false
}

func toUpper(c byte) byte {
	if 'a' <= c && c <= 'z' {
		return c - ('a' - 'A')
	}
	return c
}

// postClean adjusts the results of Clean to avoid turning a relative path
// into an absolute or rooted one.
func (os windowsInfo) postClean(out *lazybuf) {
	if out.volLen != 0 || out.buf == nil {
		return
	}
	// If a ':' appears in the path element at the start of a path,
	// insert a .\ at the beginning to avoid converting relative paths
	// like a/../c: into c:.
	for _, c := range out.buf[:out.w] {
		if isSlash(c) {
			break
		}
		if c == ':' {
			out.prepend('.', windowsSeparator)
			return
		}
	}
	// If a path begins with \??\, insert a \. at the beginning
	// to avoid converting paths like \a\..\??\c:\x into \??\c:\x
	// (equivalent to c:\x).
	if out.w >= 3 && isSlash(out.buf[0]) && out.buf[1] == '?' && out.buf[2] == '?' {
		out.prepend(windowsSeparator, '.')
	}
}

func (os windowsInfo) splitList(path string) []string {
	// The same implementation is used in LookPath in os/exec;
	// consider changing os/exec when changing this.

	if path == "" {
		return []string{}
	}

	// Split path, respecting but preserving quotes.
	list := []string{}
	start := 0
	quo := false
	for i := 0; i < len(path); i++ {
		switch c := path[i]; {
		case c == '"':
			quo = !quo
		case c == windowsListSeparator && !quo:
			list = append(list, path[start:i])
			start = i + 1
		}
	}
	list = append(list, path[start:])

	// Remove quotes.
	for i, s := range list {
		list[i] = strings.ReplaceAll(s, `"`, ``)
	}

	return list
}

func (os windowsInfo) join(elem []string) string {
	var b strings.Builder
	var lastChar byte
	for _, e := range elem {
		switch {
		case b.Len() == 0:
			// Add the first non-empty path element unchanged.
		case isSlash(lastChar):
			// If the path ends in a slash, strip any leading slashes from the next
			// path element to avoid creating a UNC path (any path starting with "\\")
			// from non-UNC elements.
			//
			// The correct behavior for Join when the first element is an incomplete UNC
			// path (for example, "\\") is underspecified. We currently join subsequent
			// elements so Join("\\", "host", "share") produces "\\host\share".
			for len(e) > 0 && isSlash(e[0]) {
				e = e[1:]
			}
			// If the path is \ and the next path element is ??,
			// add an extra .\ to create \.\?? rather than \??\
			// (a Root Local Device path).
			if b.Len() == 1 && strings.HasPrefix(e, "??") && (len(e) == len("??") || isSlash(e[2])) {
				b.WriteString(`.\`)
			}
		case lastChar == ':':
			// If the path ends in a colon, keep the path relative to the current directory
			// on a drive and don't add a separator. Preserve leading slashes in the next
			// path element, which may make the path absolute.
			//
			// 	Join(`C:`, `f`) = `C:f`
			//	Join(`C:`, `\f`) = `C:\f`
		default:
			// In all other cases, add a separator between elements.
			b.WriteByte(windowsSeparator)
			lastChar = windowsSeparator
		}
		if len(e) > 0 {
			b.WriteString(e)
			lastChar = e[len(e)-1]
		}
	}
	if b.Len() == 0 {
		return ""
	}
	return clean(b.String(), windows)
}

func (o windowsInfo) sameWord(a, b string) bool {
	return strings.EqualFold(a, b)
}
