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

package path

import (
	"reflect"
	"runtime"
	"testing"
)

type PathTest struct {
	path, result string
}

var cleantests = []PathTest{
	// Already clean
	{"abc", "abc"},
	{"abc/def", "abc/def"},
	{"a/b/c", "a/b/c"},
	{".", "."},
	{"..", ".."},
	{"../..", "../.."},
	{"../../abc", "../../abc"},
	{"/abc", "/abc"},
	{"/", "/"},

	// Empty is current dir
	{"", "."},

	// Remove trailing slash
	{"abc/", "abc"},
	{"abc/def/", "abc/def"},
	{"a/b/c/", "a/b/c"},
	{"./", "."},
	{"../", ".."},
	{"../../", "../.."},
	{"/abc/", "/abc"},

	// Remove doubled slash
	{"abc//def//ghi", "abc/def/ghi"},
	{"//abc", "/abc"},
	{"///abc", "/abc"},
	{"//abc//", "/abc"},
	{"abc//", "abc"},

	// Remove . elements
	{"abc/./def", "abc/def"},
	{"/./abc/def", "/abc/def"},
	{"abc/.", "abc"},

	// Remove .. elements
	{"abc/def/ghi/../jkl", "abc/def/jkl"},
	{"abc/def/../ghi/../jkl", "abc/jkl"},
	{"abc/def/..", "abc"},
	{"abc/def/../..", "."},
	{"/abc/def/../..", "/"},
	{"abc/def/../../..", ".."},
	{"/abc/def/../../..", "/"},
	{"abc/def/../../../ghi/jkl/../../../mno", "../../mno"},
	{"/../abc", "/abc"},

	// Combinations
	{"abc/./../def", "def"},
	{"abc//./../def", "def"},
	{"abc/../../././../def", "../../def"},
}

var wincleantests = []PathTest{
	{`c:`, `c:.`},
	{`c:\`, `c:\`},
	{`c:\abc`, `c:\abc`},
	{`c:abc\..\..\.\.\..\def`, `c:..\..\def`},
	{`c:\abc\def\..\..`, `c:\`},
	{`c:\..\abc`, `c:\abc`},
	{`c:..\abc`, `c:..\abc`},
	{`\`, `\`},
	{`/`, `\`},
	{`\\i\..\c$`, `\c$`},
	{`\\i\..\i\c$`, `\i\c$`},
	{`\\i\..\I\c$`, `\I\c$`},
	{`\\host\share\foo\..\bar`, `\\host\share\bar`},
	{`//host/share/foo/../baz`, `\\host\share\baz`},
	{`\\a\b\..\c`, `\\a\b\c`},
	{`\\a\b`, `\\a\b`},
}

func TestClean(t *testing.T) {
	tests := cleantests
	for _, os := range []OS{Unix, Windows, Plan9} {
		if os == Windows {
			for i := range tests {
				tests[i].result = FromSlash(tests[i].result, os)
			}
			tests = append(tests, wincleantests...)
		}
		for _, test := range tests {
			if s := Clean(test.path, os); s != test.result {
				t.Errorf("Clean(%q) = %q, want %q", test.path, s, test.result)
			}
			if s := Clean(test.result, os); s != test.result {
				t.Errorf("Clean(%q) = %q, want %q", test.result, s, test.result)
			}
		}

		if testing.Short() {
			t.Skip("skipping malloc count in short mode")
		}
		if runtime.GOMAXPROCS(0) > 1 {
			t.Log("skipping AllocsPerRun checks; GOMAXPROCS>1")
			return
		}

		for _, test := range tests {
			allocs := testing.AllocsPerRun(100, func() { Clean(test.result, os) })
			if allocs > 0 {
				t.Errorf("Clean(%q): %v allocs, want zero", test.result, allocs)
			}
		}
	}
}

func TestFromAndToSlash(t *testing.T) {
	for _, o := range []OS{Unix, Windows, Plan9} {
		sep := getOS(o).Separator

		var slashtests = []PathTest{
			{"", ""},
			{"/", string(sep)},
			{"/a/b", string([]byte{sep, 'a', sep, 'b'})},
			{"a//b", string([]byte{'a', sep, sep, 'b'})},
		}

		for _, test := range slashtests {
			if s := FromSlash(test.path, o); s != test.result {
				t.Errorf("FromSlash(%q) = %q, want %q", test.path, s, test.result)
			}
			if s := ToSlash(test.result, o); s != test.path {
				t.Errorf("ToSlash(%q) = %q, want %q", test.result, s, test.path)
			}
		}
	}
}

type SplitListTest struct {
	list   string
	result []string
}

var winsplitlisttests = []SplitListTest{
	// quoted
	{`"a"`, []string{`a`}},

	// semicolon
	{`";"`, []string{`;`}},
	{`"a;b"`, []string{`a;b`}},
	{`";";`, []string{`;`, ``}},
	{`;";"`, []string{``, `;`}},

	// partially quoted
	{`a";"b`, []string{`a;b`}},
	{`a; ""b`, []string{`a`, ` b`}},
	{`"a;b`, []string{`a;b`}},
	{`""a;b`, []string{`a`, `b`}},
	{`"""a;b`, []string{`a;b`}},
	{`""""a;b`, []string{`a`, `b`}},
	{`a";b`, []string{`a;b`}},
	{`a;b";c`, []string{`a`, `b;c`}},
	{`"a";b";c`, []string{`a`, `b;c`}},
}

func TestSplitList(t *testing.T) {
	for _, os := range []OS{Unix, Windows, Plan9} {
		sep := getOS(os).ListSeparator

		tests := []SplitListTest{
			{"", []string{}},
			{string([]byte{'a', sep, 'b'}), []string{"a", "b"}},
			{string([]byte{sep, 'a', sep, 'b'}), []string{"", "a", "b"}},
		}
		if os == Windows {
			tests = append(tests, winsplitlisttests...)
		}
		for _, test := range tests {
			if l := SplitList(test.list, os); !reflect.DeepEqual(l, test.result) {
				t.Errorf("SplitList(%#q, %q) = %#q, want %#q", test.list, os, l, test.result)
			}
		}
	}
}

type SplitTest struct {
	path, dir, file string
}

var unixsplittests = []SplitTest{
	{"a/b", "a/", "b"},
	{"a/b/", "a/b/", ""},
	{"a/", "a/", ""},
	{"a", "", "a"},
	{"/", "/", ""},
}

var winsplittests = []SplitTest{
	{`c:`, `c:`, ``},
	{`c:/`, `c:/`, ``},
	{`c:/foo`, `c:/`, `foo`},
	{`c:/foo/bar`, `c:/foo/`, `bar`},
	{`//host/share`, `//host/share`, ``},
	{`//host/share/`, `//host/share/`, ``},
	{`//host/share/foo`, `//host/share/`, `foo`},
	{`\\host\share`, `\\host\share`, ``},
	{`\\host\share\`, `\\host\share\`, ``},
	{`\\host\share\foo`, `\\host\share\`, `foo`},
}

func TestSplit(t *testing.T) {
	for _, os := range []OS{Windows, Unix} {
		var splittests []SplitTest
		splittests = unixsplittests
		if os == Windows {
			splittests = append(splittests, winsplittests...)
		}
		for _, test := range splittests {
			pair := Split(test.path, os)
			d, f := pair[0], pair[1]
			if d != test.dir || f != test.file {
				t.Errorf("Split(%q, %q) = %q, %q, want %q, %q",
					test.path, os, d, f, test.dir, test.file)
			}
		}
	}
}

type JoinTest struct {
	elem []string
	path string
}

var jointests = []JoinTest{
	// zero parameters
	{[]string{}, ""},

	// one parameter
	{[]string{""}, ""},
	{[]string{"/"}, "/"},
	{[]string{"a"}, "a"},

	// two parameters
	{[]string{"a", "b"}, "a/b"},
	{[]string{"a", ""}, "a"},
	{[]string{"", "b"}, "b"},
	{[]string{"/", "a"}, "/a"},
	{[]string{"/", "a/b"}, "/a/b"},
	{[]string{"/", ""}, "/"},
	{[]string{"//", "a"}, "/a"},
	{[]string{"/a", "b"}, "/a/b"},
	{[]string{"a/", "b"}, "a/b"},
	{[]string{"a/", ""}, "a"},
	{[]string{"", ""}, ""},

	// three parameters
	{[]string{"/", "a", "b"}, "/a/b"},
}

var winjointests = []JoinTest{
	{[]string{`directory`, `file`}, `directory\file`},
	{[]string{`C:\Windows\`, `System32`}, `C:\Windows\System32`},
	{[]string{`C:\Windows\`, ``}, `C:\Windows`},
	{[]string{`C:\`, `Windows`}, `C:\Windows`},
	{[]string{`C:`, `a`}, `C:a`},
	{[]string{`C:`, `a\b`}, `C:a\b`},
	{[]string{`C:`, `a`, `b`}, `C:a\b`},
	{[]string{`C:`, ``, `b`}, `C:b`},
	{[]string{`C:`, ``, ``, `b`}, `C:b`},
	{[]string{`C:`, ``}, `C:.`},
	{[]string{`C:`, ``, ``}, `C:.`},
	{[]string{`C:.`, `a`}, `C:a`},
	{[]string{`C:a`, `b`}, `C:a\b`},
	{[]string{`C:a`, `b`, `d`}, `C:a\b\d`},
	{[]string{`\\host\share`, `foo`}, `\\host\share\foo`},
	{[]string{`\\host\share\foo`}, `\\host\share\foo`},
	{[]string{`//host/share`, `foo/bar`}, `\\host\share\foo\bar`},
	{[]string{`\`}, `\`},
	{[]string{`\`, ``}, `\`},
	{[]string{`\`, `a`}, `\a`},
	{[]string{`\\`, `a`}, `\a`},
	{[]string{`\`, `a`, `b`}, `\a\b`},
	{[]string{`\\`, `a`, `b`}, `\a\b`},
	{[]string{`\`, `\\a\b`, `c`}, `\a\b\c`},
	{[]string{`\\a`, `b`, `c`}, `\a\b\c`},
	{[]string{`\\a\`, `b`, `c`}, `\a\b\c`},
}

func TestJoin(t *testing.T) {
	for _, os := range []OS{Unix, Windows} {
		if os == Windows {
			jointests = append(jointests, winjointests...)
		}
		for _, test := range jointests {
			expected := FromSlash(test.path, os)
			if p := Join(test.elem, os); p != expected {
				t.Errorf("join(%q, %q) = %q, want %q", test.elem, os, p, expected)
			}
		}
	}
}

type ExtTest struct {
	path, ext string
}

var exttests = []ExtTest{
	{"path.go", ".go"},
	{"path.pb.go", ".go"},
	{"a.dir/b", ""},
	{"a.dir/b.go", ".go"},
	{"a.dir/", ""},
}

func TestExt(t *testing.T) {
	for _, os := range []OS{Unix, Windows} {
		for _, test := range exttests {
			if x := Ext(test.path, os); x != test.ext {
				t.Errorf("Ext(%q, %q) = %q, want %q", test.path, os, x, test.ext)
			}
		}
	}
}

var basetests = []PathTest{
	{"", "."},
	{".", "."},
	{"/.", "."},
	{"/", "/"},
	{"////", "/"},
	{"x/", "x"},
	{"abc", "abc"},
	{"abc/def", "def"},
	{"a/b/.x", ".x"},
	{"a/b/c.", "c."},
	{"a/b/c.x", "c.x"},
}

var winbasetests = []PathTest{
	{`c:\`, `\`},
	{`c:.`, `.`},
	{`c:\a\b`, `b`},
	{`c:a\b`, `b`},
	{`c:a\b\c`, `c`},
	{`\\host\share\`, `\`},
	{`\\host\share\a`, `a`},
	{`\\host\share\a\b`, `b`},
}

func TestBase(t *testing.T) {
	tests := basetests
	for _, os := range []OS{Unix, Windows} {
		if os == Windows {
			// make unix tests work on windows
			for i := range tests {
				tests[i].result = Clean(tests[i].result, os)
			}
			// add windows specific tests
			tests = append(tests, winbasetests...)
		}
		for _, test := range tests {
			if s := Base(test.path, os); s != test.result {
				t.Errorf("Base(%q, %q) = %q, want %q", test.path, os, s, test.result)
			}
		}
	}
}

var dirtests = []PathTest{
	{"", "."},
	{".", "."},
	{"/.", "/"},
	{"/", "/"},
	{"////", "/"},
	{"/foo", "/"},
	{"x/", "x"},
	{"abc", "."},
	{"abc/def", "abc"},
	{"a/b/.x", "a/b"},
	{"a/b/c.", "a/b"},
	{"a/b/c.x", "a/b"},
}

var windirtests = []PathTest{
	{`c:\`, `c:\`},
	{`c:.`, `c:.`},
	{`c:\a\b`, `c:\a`},
	{`c:a\b`, `c:a`},
	{`c:a\b\c`, `c:a\b`},
	{`\\host\share`, `\\host\share`},
	{`\\host\share\`, `\\host\share\`},
	{`\\host\share\a`, `\\host\share\`},
	{`\\host\share\a\b`, `\\host\share\a`},
}

func TestDir(t *testing.T) {
	for _, os := range []OS{Unix, Windows} {
		tests := dirtests
		if os == Windows {
			// make unix tests work on windows
			for i := range tests {
				tests[i].result = Clean(tests[i].result, os)
			}
			// add windows specific tests
			tests = append(tests, windirtests...)
		}
		for _, test := range tests {
			if s := Dir(test.path, os); s != test.result {
				t.Errorf("Dir(%q, %q) = %q, want %q", test.path, os, s, test.result)
			}
		}
	}
}

type IsAbsTest struct {
	path  string
	isAbs bool
}

var isabstests = []IsAbsTest{
	{"", false},
	{"/", true},
	{"/usr/bin/gcc", true},
	{"..", false},
	{"/a/../bb", true},
	{".", false},
	{"./", false},
	{"lala", false},
}

var winisabstests = []IsAbsTest{
	{`C:\`, true},
	{`c\`, false},
	{`c::`, false},
	{`c:`, false},
	{`/`, false},
	{`\`, false},
	{`\Windows`, false},
	{`c:a\b`, false},
	{`c:\a\b`, true},
	{`c:/a/b`, true},
	{`\\host\share\foo`, true},
	{`//host/share/foo/bar`, true},
}

func TestIsAbs(t *testing.T) {
	for _, os := range []OS{Unix, Windows} {
		var tests []IsAbsTest
		if os == Windows {
			tests = append(tests, winisabstests...)
			// All non-windows tests should fail, because they have no volume letter.
			for _, test := range isabstests {
				tests = append(tests, IsAbsTest{test.path, false})
			}
			// All non-windows test should work as intended if prefixed with volume letter.
			for _, test := range isabstests {
				tests = append(tests, IsAbsTest{"c:" + test.path, test.isAbs})
			}
			// Test reserved names.
			// tests = append(tests, IsAbsTest{"/dev/null", true})
			tests = append(tests, IsAbsTest{"NUL", true})
			tests = append(tests, IsAbsTest{"nul", true})
			tests = append(tests, IsAbsTest{"CON", true})
		} else {
			tests = isabstests
		}

		for _, test := range tests {
			if r := IsAbs(test.path, os); r != test.isAbs {
				t.Errorf("IsAbs(%q, %q) = %v, want %v", test.path, os, r, test.isAbs)
			}
		}
	}
}

// // simpleJoin builds a file name from the directory and path.
// // It does not use Join because we don't want ".." to be evaluated.
// func simpleJoin(dir, path string) string {
// 	return dir + string(Separator) + path
// }

// Test directories relative to temporary directory.
// The tests are run in absTestDirs[0].
var absTestDirs = []string{
	"a",
	"a/b",
	"a/b/c",
}

// Test paths relative to temporary directory. $ expands to the directory.
// The tests are run in absTestDirs[0].
// We create absTestDirs first.
var absTests = []string{
	".",
	"b",
	"b/",
	"../a",
	"../a/b",
	"../a/b/./c/../../.././a",
	"../a/b/./c/../../.././a/",
	"$",
	"$/.",
	"$/a/../a/b",
	"$/a/b/c/../../.././a",
	"$/a/b/c/../../.././a/",
}

type RelTests struct {
	root, path, want string
}

var reltests = []RelTests{
	{"a/b", "a/b", "."},
	{"a/b/.", "a/b", "."},
	{"a/b", "a/b/.", "."},
	{"./a/b", "a/b", "."},
	{"a/b", "./a/b", "."},
	{"ab/cd", "ab/cde", "../cde"},
	{"ab/cd", "ab/c", "../c"},
	{"a/b", "a/b/c/d", "c/d"},
	{"a/b", "a/b/../c", "../c"},
	{"a/b/../c", "a/b", "../b"},
	{"a/b/c", "a/c/d", "../../c/d"},
	{"a/b", "c/d", "../../c/d"},
	{"a/b/c/d", "a/b", "../.."},
	{"a/b/c/d", "a/b/", "../.."},
	{"a/b/c/d/", "a/b", "../.."},
	{"a/b/c/d/", "a/b/", "../.."},
	{"../../a/b", "../../a/b/c/d", "c/d"},
	{"/a/b", "/a/b", "."},
	{"/a/b/.", "/a/b", "."},
	{"/a/b", "/a/b/.", "."},
	{"/ab/cd", "/ab/cde", "../cde"},
	{"/ab/cd", "/ab/c", "../c"},
	{"/a/b", "/a/b/c/d", "c/d"},
	{"/a/b", "/a/b/../c", "../c"},
	{"/a/b/../c", "/a/b", "../b"},
	{"/a/b/c", "/a/c/d", "../../c/d"},
	{"/a/b", "/c/d", "../../c/d"},
	{"/a/b/c/d", "/a/b", "../.."},
	{"/a/b/c/d", "/a/b/", "../.."},
	{"/a/b/c/d/", "/a/b", "../.."},
	{"/a/b/c/d/", "/a/b/", "../.."},
	{"/../../a/b", "/../../a/b/c/d", "c/d"},
	{".", "a/b", "a/b"},
	{".", "..", ".."},

	// can't do purely lexically
	{"..", ".", "err"},
	{"..", "a", "err"},
	{"../..", "..", "err"},
	{"a", "/a", "err"},
	{"/a", "a", "err"},
}

var winreltests = []RelTests{
	{`C:a\b\c`, `C:a/b/d`, `..\d`},
	{`C:\`, `D:\`, `err`},
	{`C:`, `D:`, `err`},
	{`C:\Projects`, `c:\projects\src`, `src`},
	{`C:\Projects`, `c:\projects`, `.`},
	{`C:\Projects\a\..`, `c:\projects`, `.`},
}

func TestRel(t *testing.T) {
	for _, os := range []OS{Unix, Windows} {
		tests := append([]RelTests{}, reltests...)
		if os == Windows {
			for i := range tests {
				tests[i].want = FromSlash(tests[i].want, Windows)
			}
			tests = append(tests, winreltests...)
		}
		for _, test := range tests {
			got, err := Rel(test.root, test.path, os)
			if test.want == "err" {
				if err == nil {
					t.Errorf("Rel(%q, %q, %q)=%q, want error", test.root, test.path, os, got)
				}
				continue
			}
			if err != nil {
				t.Errorf("Rel(%q, %q, %q): want %q, got error: %s", test.root, test.path, os, test.want, err)
			}
			if got != test.want {
				t.Errorf("Rel(%q, %q, %q)=%q, want %q", test.root, test.path, os, got, test.want)
			}
		}
	}
}

type VolumeNameTest struct {
	path string
	vol  string
}

var volumenametests = []VolumeNameTest{
	{`c:/foo/bar`, `c:`},
	{`c:`, `c:`},
	{`2:`, ``},
	{``, ``},
	{`\\\host`, ``},
	{`\\\host\`, ``},
	{`\\\host\share`, ``},
	{`\\\host\\share`, ``},
	{`\\host`, ``},
	{`//host`, ``},
	{`\\host\`, ``},
	{`//host/`, ``},
	{`\\host\share`, `\\host\share`},
	{`//host/share`, `//host/share`},
	{`\\host\share\`, `\\host\share`},
	{`//host/share/`, `//host/share`},
	{`\\host\share\foo`, `\\host\share`},
	{`//host/share/foo`, `//host/share`},
	{`\\host\share\\foo\\\bar\\\\baz`, `\\host\share`},
	{`//host/share//foo///bar////baz`, `//host/share`},
	{`\\host\share\foo\..\bar`, `\\host\share`},
	{`//host/share/foo/../bar`, `//host/share`},
}

func TestVolumeName(t *testing.T) {
	for _, os := range []OS{Unix, Windows} {
		if os != Windows {
			return
		}
		for _, v := range volumenametests {
			if vol := VolumeName(v.path, os); vol != v.vol {
				t.Errorf("VolumeName(%q, %q)=%q, want %q", v.path, os, vol, v.vol)
			}
		}
	}
}
