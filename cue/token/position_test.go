// Copyright 2018 The CUE Authors
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

package token

import (
	"fmt"
	"testing"
)

func checkPos(t *testing.T, msg string, got, want Position) {
	if got.Filename != want.Filename {
		t.Errorf("%s: got filename = %q; want %q", msg, got.Filename, want.Filename)
	}
	if got.Offset != want.Offset {
		t.Errorf("%s: got offset = %d; want %d", msg, got.Offset, want.Offset)
	}
	if got.Line != want.Line {
		t.Errorf("%s: got line = %d; want %d", msg, got.Line, want.Line)
	}
	if got.Column != want.Column {
		t.Errorf("%s: got column = %d; want %d", msg, got.Column, want.Column)
	}
}

func TestNoPos(t *testing.T) {
	if NoPos.IsValid() {
		t.Errorf("NoPos should not be valid")
	}
	checkPos(t, "nil NoPos", NoPos.Position(), Position{})
}

var tests = []struct {
	filename string
	source   []byte // may be nil
	size     int
	lines    []int
}{
	{"a", []byte{}, 0, []int{}},
	{"b", []byte("01234"), 5, []int{0}},
	{"c", []byte("\n\n\n\n\n\n\n\n\n"), 9, []int{0, 1, 2, 3, 4, 5, 6, 7, 8}},
	{"d", nil, 100, []int{0, 5, 10, 20, 30, 70, 71, 72, 80, 85, 90, 99}},
	{"e", nil, 777, []int{0, 80, 100, 120, 130, 180, 267, 455, 500, 567, 620}},
	{"f", []byte("package p\n\nimport \"fmt\""), 23, []int{0, 10, 11}},
	{"g", []byte("package p\n\nimport \"fmt\"\n"), 24, []int{0, 10, 11}},
	{"h", []byte("package p\n\nimport \"fmt\"\n "), 25, []int{0, 10, 11, 24}},
}

func linecol(lines []int, offs int) (int, int) {
	prevLineOffs := 0
	for line, lineOffs := range lines {
		if offs < lineOffs {
			return line, offs - prevLineOffs + 1
		}
		prevLineOffs = lineOffs
	}
	return len(lines), offs - prevLineOffs + 1
}

func verifyPositions(t *testing.T, f *File, lines []int) {
	for offs := 0; offs < f.Size(); offs++ {
		p := f.Pos(offs, 0)
		offs2 := f.Offset(p)
		if offs2 != offs {
			t.Errorf("%s, Offset: got offset %d; want %d", f.Name(), offs2, offs)
		}
		line, col := linecol(lines, offs)
		msg := fmt.Sprintf("%s (offs = %d, p = %d)", f.Name(), offs, p.offset)
		checkPos(t, msg, f.Pos(offs, 0).Position(), Position{f.Name(), offs, line, col})
		checkPos(t, msg, p.Position(), Position{f.Name(), offs, line, col})
	}
}

func makeTestSource(size int, lines []int) []byte {
	src := make([]byte, size)
	for _, offs := range lines {
		if offs > 0 {
			src[offs-1] = '\n'
		}
	}
	return src
}

func TestPositions(t *testing.T) {
	const delta = 7 // a non-zero base offset increment
	for _, test := range tests {
		// verify consistency of test case
		if test.source != nil && len(test.source) != test.size {
			t.Errorf("%s: inconsistent test case: got file size %d; want %d", test.filename, len(test.source), test.size)
		}

		// add file and verify name and size
		f := NewFile(test.filename, 1+delta, test.size)
		if f.Name() != test.filename {
			t.Errorf("got filename %q; want %q", f.Name(), test.filename)
		}
		if f.Size() != test.size {
			t.Errorf("%s: got file size %d; want %d", f.Name(), f.Size(), test.size)
		}
		if f.Pos(0, 0).file != f {
			t.Errorf("%s: f.Pos(0, 0) was not found in f", f.Name())
		}

		// add lines individually and verify all positions
		for i, offset := range test.lines {
			f.AddLine(offset)
			if f.LineCount() != i+1 {
				t.Errorf("%s, AddLine: got line count %d; want %d", f.Name(), f.LineCount(), i+1)
			}
			// adding the same offset again should be ignored
			f.AddLine(offset)
			if f.LineCount() != i+1 {
				t.Errorf("%s, AddLine: got unchanged line count %d; want %d", f.Name(), f.LineCount(), i+1)
			}
			verifyPositions(t, f, test.lines[0:i+1])
		}

		// add lines with SetLines and verify all positions
		if ok := f.SetLines(test.lines); !ok {
			t.Errorf("%s: SetLines failed", f.Name())
		}
		if f.LineCount() != len(test.lines) {
			t.Errorf("%s, SetLines: got line count %d; want %d", f.Name(), f.LineCount(), len(test.lines))
		}
		verifyPositions(t, f, test.lines)

		// add lines with SetLinesForContent and verify all positions
		src := test.source
		if src == nil {
			// no test source available - create one from scratch
			src = makeTestSource(test.size, test.lines)
		}
		f.SetLinesForContent(src)
		if f.LineCount() != len(test.lines) {
			t.Errorf("%s, SetLinesForContent: got line count %d; want %d", f.Name(), f.LineCount(), len(test.lines))
		}
		verifyPositions(t, f, test.lines)
	}
}

func TestLineInfo(t *testing.T) {
	f := NewFile("foo", 1, 500)
	lines := []int{0, 42, 77, 100, 210, 220, 277, 300, 333, 401}
	// add lines individually and provide alternative line information
	for _, offs := range lines {
		f.AddLine(offs)
		f.AddLineInfo(offs, "bar", 42)
	}
	// verify positions for all offsets
	for offs := 0; offs <= f.Size(); offs++ {
		p := f.Pos(offs, 0)
		_, col := linecol(lines, offs)
		msg := fmt.Sprintf("%s (offs = %d, p = %d)", f.Name(), offs, p.offset)
		checkPos(t, msg, f.Position(f.Pos(offs, 0)), Position{"bar", offs, 42, col})
		checkPos(t, msg, p.Position(), Position{"bar", offs, 42, col})
	}
}

func TestPositionFor(t *testing.T) {
	src := []byte(`
foo
b
ar
//line :100
foobar
//line bar:3
done
`)

	const filename = "foo"
	f := NewFile(filename, 1, len(src))
	f.SetLinesForContent(src)

	// verify position info
	for i, offs := range f.lines {
		got1 := f.PositionFor(f.Pos(int(offs), 0), false)
		got2 := f.PositionFor(f.Pos(int(offs), 0), true)
		got3 := f.Position(f.Pos(int(offs), 0))
		want := Position{filename, int(offs), i + 1, 1}
		checkPos(t, "1. PositionFor unadjusted", got1, want)
		checkPos(t, "1. PositionFor adjusted", got2, want)
		checkPos(t, "1. Position", got3, want)
	}

	// manually add //line info on lines l1, l2
	const l1, l2 = 5, 7
	f.AddLineInfo(int(f.lines[l1-1]), "", 100)
	f.AddLineInfo(int(f.lines[l2-1]), "bar", 3)

	// unadjusted position info must remain unchanged
	for i, offs := range f.lines {
		got1 := f.PositionFor(f.Pos(int(offs), 0), false)
		want := Position{filename, int(offs), i + 1, 1}
		checkPos(t, "2. PositionFor unadjusted", got1, want)
	}

	// adjusted position info should have changed
	for i, offs := range f.lines {
		got2 := f.PositionFor(f.Pos(int(offs), 0), true)
		got3 := f.Position(f.Pos(int(offs), 0))
		want := Position{filename, int(offs), i + 1, 1}
		// manually compute wanted filename and line
		line := want.Line
		if i+1 >= l1 {
			want.Filename = ""
			want.Line = line - l1 + 100
		}
		if i+1 >= l2 {
			want.Filename = "bar"
			want.Line = line - l2 + 3
		}
		checkPos(t, "3. PositionFor adjusted", got2, want)
		checkPos(t, "3. Position", got3, want)
	}
}
