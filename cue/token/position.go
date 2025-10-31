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
	"cmp"
	"fmt"
	"sort"
	"sync"

	"cuelang.org/go/internal/core/layer"
	"cuelang.org/go/internal/cueexperiment"
)

// -----------------------------------------------------------------------------
// Positions

// Position describes an arbitrary and printable source position within a file,
// including offset, line, and column location,
// which can be rendered in a human-friendly text form.
//
// A Position is valid if the line number is > 0.
type Position struct {
	Filename string // filename, if any
	Offset   int    // offset, starting at 0
	Line     int    // line number, starting at 1
	Column   int    // column number, starting at 1 (byte count)
	// RelPos   Pos // relative position information
}

// IsValid reports whether the position is valid.
func (pos *Position) IsValid() bool { return pos.Line > 0 }

// String returns a human-readable form of a position in one of several forms:
//
//	file:line:column    valid position with file name
//	line:column         valid position without file name
//	file                invalid position with file name
//	-                   invalid position without file name
func (pos Position) String() string {
	s := pos.Filename
	if pos.IsValid() {
		if s != "" {
			s += ":"
		}
		s += fmt.Sprintf("%d:%d", pos.Line, pos.Column)
	}
	if s == "" {
		s = "-"
	}
	return s
}

// Pos is a compact encoding of a source position.
// When valid, as reported by [Pos.IsValid], this can be either
// a printable file position to obtain via [Pos.Position],
// which can be rendered in a human-friendly text form,
// and/or a relative position to obtain via [Pos.RelPos].
type Pos struct {
	file   *File
	offset int
}

// File returns the file that contains the printable position p
// or nil if there is no such file (for instance for p == [NoPos]).
func (p Pos) File() *File {
	if p.index() == 0 {
		return nil
	}
	return p.file
}

// hiddenPos allows defining methods in Pos that are hidden from public
// documentation.
type hiddenPos = Pos

func (p hiddenPos) Experiment() (x cueexperiment.File) {
	if p.file == nil || p.file.experiments == nil {
		return x
	}

	x = *p.file.experiments
	return x
}

// NOTE: this is an internal API and may change at any time without notice.
func (p hiddenPos) Priority() (pr layer.Priority, ok bool) {
	if f := p.file; f != nil {
		return f.priority, f.isData
	}
	return 0, false
}

// Line returns the position's line number, starting at 1.
func (p Pos) Line() int {
	return p.Position().Line
}

// Column returns the position's column number counting in bytes,
// starting at 1.
func (p Pos) Column() int {
	return p.Position().Column
}

// Filename returns the name of the file that this position belongs to.
func (p Pos) Filename() string {
	// Avoid calling [Pos.Position] as it also unpacks line and column info.
	if p.file == nil {
		return ""
	}
	return p.file.name
}

// Position unpacks the position information into a flat struct.
func (p Pos) Position() Position {
	if p.file == nil {
		return Position{}
	}
	return p.file.Position(p)
}

// String returns a human-readable form of a printable position.
func (p Pos) String() string {
	return p.Position().String()
}

// Compare returns an integer comparing two positions. The result will be 0 if p == p2,
// -1 if p < p2, and +1 if p > p2. Note that [NoPos] is always larger than any valid position.
func (p Pos) Compare(p2 Pos) int {
	if p == p2 {
		return 0
	} else if p == NoPos {
		return +1
	} else if p2 == NoPos {
		return -1
	}
	// Avoid calling [Pos.Position] as it also unpacks line and column info;
	// comparing positions only needs filenames and offsets.
	if c := cmp.Compare(p.Filename(), p2.Filename()); c != 0 {
		return c
	}
	// Note that CUE doesn't currently use any directives which alter
	// position information, like Go's //line, so comparing by offset is enough.
	return cmp.Compare(p.Offset(), p2.Offset())
}

// NoPos is the zero value for [Pos]; there is no file and line information
// associated with it, and [Pos.IsValid] is false.
//
// NoPos is always larger than any valid [Pos] value, as it tends to relate
// to values produced from evaluating existing values with valid positions.
// The corresponding [Position] value for NoPos is the zero value.
var NoPos = Pos{}

// RelPos indicates the relative position of token to the previous token.
type RelPos int

//go:generate go tool stringer -type=RelPos -linecomment

const (
	// NoRelPos indicates no relative position is specified.
	NoRelPos RelPos = iota // invalid

	// Elided indicates that the token for which this position is defined is
	// not rendered at all.
	Elided // elided

	// NoSpace indicates there is no whitespace before this token.
	NoSpace // nospace

	// Blank means there is horizontal space before this token.
	Blank // blank

	// Newline means there is a single newline before this token.
	Newline // newline

	// NewSection means there are two or more newlines before this token.
	NewSection // section

	relMask  = 0xf
	relShift = 4
)

func (p RelPos) Pos() Pos {
	return Pos{nil, int(p)}
}

// HasRelPos reports whether p has a relative position.
func (p Pos) HasRelPos() bool {
	return p.offset&relMask != 0
}

// Before reports whether p < q, as documented in [Pos.Compare].
//
// Deprecated: use [Pos.Compare] instead.
//
//go:fix inline
func (p Pos) Before(q Pos) bool {
	return p.Compare(q) < 0
}

// Offset reports the byte offset relative to the file.
func (p Pos) Offset() int {
	// Avoid calling [Pos.Position] as it also unpacks line and column info.
	if p.file == nil {
		return 0
	}
	return p.file.Offset(p)
}

// Add creates a new position relative to the p offset by n.
func (p Pos) Add(n int) Pos {
	return Pos{p.file, p.offset + toPos(index(n))}
}

// IsValid reports whether the position contains any useful information,
// meaning either a printable file position to obtain via [Pos.Position],
// and/or a relative position to obtain via [Pos.RelPos].
func (p Pos) IsValid() bool {
	return p != NoPos
}

// IsNewline reports whether the relative information suggests this node should
// be printed on a new line.
func (p Pos) IsNewline() bool {
	return p.RelPos() >= Newline
}

func (p Pos) WithRel(rel RelPos) Pos {
	return Pos{p.file, p.offset&^relMask | int(rel)}
}

func (p Pos) RelPos() RelPos {
	return RelPos(p.offset & relMask)
}

func (p Pos) index() index {
	return index(p.offset) >> relShift
}

func toPos(x index) int {
	return (int(x) << relShift)
}

// -----------------------------------------------------------------------------
// File

// index represents an offset into the file.
// It's 1-based rather than zero-based so that
// we can distinguish the zero Pos from a Pos that
// just has a zero offset.
type index int

// A File has a name, size, and line offset table.
type File struct {
	mutex sync.RWMutex
	name  string // file name as provided to AddFile
	// base is deprecated and stored only so that [File.Base]
	// can continue to return the same value passed to [NewFile].
	base index
	size index // file size as provided to AddFile

	// lines, infos, content, and revision are protected by [File.mutex]
	lines    []index // lines contains the offset of the first character for each line (the first entry is always 0)
	infos    []lineInfo
	content  []byte
	revision int32

	experiments *cueexperiment.File
	priority    layer.Priority
	isData      bool
}

// NewFile returns a new file with the given OS file name. The size provides the
// size of the whole file.
//
// The second argument is deprecated. It has no effect.
func NewFile(filename string, deprecatedBase, size int) *File {
	if deprecatedBase < 0 {
		deprecatedBase = 1
	}
	return &File{
		name:  filename,
		base:  index(deprecatedBase),
		size:  index(size),
		lines: []index{0},
	}
}

// fixOffset fixes an out-of-bounds offset such that 0 <= offset <= f.size.
func (f *File) fixOffset(offset index) index {
	switch {
	case offset < 0:
		return 0
	case offset > f.size:
		return f.size
	default:
		return offset
	}
}

// hiddenFile allows defining methods in File that are hidden from public
// documentation.
type hiddenFile = File

func (f *hiddenFile) SetExperiments(experiments *cueexperiment.File) {
	f.experiments = experiments
}

// NOTE: this is an internal API and may change at any time without notice.
//
// SetLayer sets the layer priority for this file. The priority parameter
// determines the precedence of defaults defined in this file, with higher
// values taking precedence over lower values. The isData parameter indicates
// whether this file should be treated as containing data defaults, which
// have different merging semantics from regular defaults.
func (f *hiddenFile) SetLayer(priority int8, isData bool) {
	f.priority = layer.Priority(priority)
	f.isData = isData
}

// Name returns the file name of file f as registered with AddFile.
func (f *File) Name() string {
	return f.name
}

// Base returns the base offset of file f as passed to NewFile.
//
// Deprecated: this method just returns the (deprecated) second argument passed to NewFile.
func (f *File) Base() int {
	return int(f.base)
}

// Size returns the size of file f as passed to NewFile.
func (f *File) Size() int {
	return int(f.size)
}

// LineCount returns the number of lines in file f.
func (f *File) LineCount() int {
	f.mutex.RLock()
	n := len(f.lines)
	f.mutex.RUnlock()
	return n
}

// AddLine adds the line offset for a new line.
// The line offset must be larger than the offset for the previous line
// and smaller than the file size; otherwise the line offset is ignored.
func (f *File) AddLine(offset int) {
	x := index(offset)
	f.mutex.Lock()
	if i := len(f.lines); (i == 0 || f.lines[i-1] < x) && x < f.size {
		f.lines = append(f.lines, x)
	}
	f.mutex.Unlock()
}

// MergeLine merges a line with the following line. It is akin to replacing
// the newline character at the end of the line with a space (to not change the
// remaining offsets). To obtain the line number, consult e.g. Position.Line.
// MergeLine will panic if given an invalid line number.
func (f *File) MergeLine(line int) {
	if line <= 0 {
		panic("illegal line number (line numbering starts at 1)")
	}
	f.mutex.Lock()
	defer f.mutex.Unlock()
	if line >= len(f.lines) {
		panic("illegal line number")
	}
	// To merge the line numbered <line> with the line numbered <line+1>,
	// we need to remove the entry in lines corresponding to the line
	// numbered <line+1>. The entry in lines corresponding to the line
	// numbered <line+1> is located at index <line>, since indices in lines
	// are 0-based and line numbers are 1-based.
	copy(f.lines[line:], f.lines[line+1:])
	f.lines = f.lines[:len(f.lines)-1]
}

// Lines returns the effective line offset table of the form described by [File.SetLines].
// Callers must not mutate the result.
func (f *File) Lines() []int {
	var lines []int
	f.mutex.Lock()
	// Unfortunate that we have to loop, but we use our own type.
	for _, line := range f.lines {
		lines = append(lines, int(line))
	}
	f.mutex.Unlock()
	return lines
}

// SetLines sets the line offsets for a file and reports whether it succeeded.
// The line offsets are the offsets of the first character of each line;
// for instance for the content "ab\nc\n" the line offsets are {0, 3}.
// An empty file has an empty line offset table.
// Each line offset must be larger than the offset for the previous line
// and smaller than the file size; otherwise SetLines fails and returns
// false.
// Callers must not mutate the provided slice after SetLines returns.
func (f *File) SetLines(lines []int) bool {
	// verify validity of lines table
	size := f.size
	for i, offset := range lines {
		if i > 0 && offset <= lines[i-1] || size <= index(offset) {
			return false
		}
	}

	// set lines table
	f.mutex.Lock()
	f.lines = f.lines[:0]
	for _, l := range lines {
		f.lines = append(f.lines, index(l))
	}
	f.mutex.Unlock()
	return true
}

// SetLinesForContent sets the line offsets for the given file content.
// It ignores position-altering //line comments.
func (f *File) SetLinesForContent(content []byte) {
	var lines []index
	line := index(0)
	for offset, b := range content {
		if line >= 0 {
			lines = append(lines, line)
		}
		line = -1
		if b == '\n' {
			line = index(offset) + 1
		}
	}

	// set lines table
	f.mutex.Lock()
	f.lines = lines
	f.mutex.Unlock()
}

// SetContent sets the file's content. The content must not be altered
// after this call.
func (f *hiddenFile) SetContent(content []byte) {
	f.mutex.Lock()
	f.content = content
	f.mutex.Unlock()
}

// Content retrievs the file's content, which may be nil. The returned
// content must not be altered.
func (f *hiddenFile) Content() []byte {
	f.mutex.RLock()
	defer f.mutex.RUnlock()
	return f.content
}

// NOTE: this is an internal API and may change at any time without notice.
//
// SetRevision sets the file's version.
func (f *hiddenFile) SetRevision(version int32) {
	f.mutex.Lock()
	f.revision = version
	f.mutex.Unlock()
}

// NOTE: this is an internal API and may change at any time without notice.
//
// Revision retrieves the file's version.
func (f *hiddenFile) Revision() int32 {
	f.mutex.RLock()
	defer f.mutex.RUnlock()
	return f.revision
}

// A lineInfo object describes alternative file and line number
// information (such as provided via a //line comment in a .go
// file) for a given file offset.
type lineInfo struct {
	// fields are exported to make them accessible to gob
	Offset   int
	Filename string
	Line     int
}

// AddLineInfo adds alternative file and line number information for
// a given file offset. The offset must be larger than the offset for
// the previously added alternative line info and smaller than the
// file size; otherwise the information is ignored.
//
// AddLineInfo is typically used to register alternative position
// information for //line filename:line comments in source files.
func (f *File) AddLineInfo(offset int, filename string, line int) {
	x := index(offset)
	f.mutex.Lock()
	if i := len(f.infos); i == 0 || index(f.infos[i-1].Offset) < x && x < f.size {
		f.infos = append(f.infos, lineInfo{offset, filename, line})
	}
	f.mutex.Unlock()
}

// Pos returns the Pos value for the given file offset.
//
// If offset is negative, the result is the file's start
// position; if the offset is too large, the result is
// the file's end position (see also go.dev/issue/57490).
//
// The following invariant, though not true for Pos values
// in general, holds for the result p:
// f.Pos(f.Offset(p)) == p.
func (f *File) Pos(offset int, rel RelPos) Pos {
	return Pos{f, toPos(1+f.fixOffset(index(offset))) + int(rel)}
}

// Offset returns the offset for the given file position p.
//
// If p is before the file's start position (or if p is NoPos),
// the result is 0; if p is past the file's end position, the
// the result is the file size (see also go.dev/issue/57490).
//
// The following invariant, though not true for offset values
// in general, holds for the result offset:
// f.Offset(f.Pos(offset)) == offset
func (f *File) Offset(p Pos) int {
	x := p.index()
	return int(f.fixOffset(x - 1))
}

// Line returns the line number for the given file position p;
// p must be a Pos value in that file or NoPos.
func (f *File) Line(p Pos) int {
	return f.Position(p).Line
}

func searchLineInfos(a []lineInfo, x int) int {
	return sort.Search(len(a), func(i int) bool { return a[i].Offset > x }) - 1
}

// unpack returns the filename and line and column number for a file offset.
// If adjusted is set, unpack will return the filename and line information
// possibly adjusted by //line comments; otherwise those comments are ignored.
func (f *File) unpack(offset index, adjusted bool) (filename string, line, column int) {
	filename = f.name
	if i := searchInts(f.lines, offset); i >= 0 {
		line, column = i+1, int(offset-f.lines[i]+1)
	}
	if adjusted && len(f.infos) > 0 {
		// almost no files have extra line infos
		if i := searchLineInfos(f.infos, int(offset)); i >= 0 {
			alt := &f.infos[i]
			filename = alt.Filename
			if i := searchInts(f.lines, index(alt.Offset)); i >= 0 {
				line += alt.Line - i - 1
			}
		}
	}
	return
}

func (f *File) position(p Pos, adjusted bool) (pos Position) {
	offset := f.Offset(p)
	pos.Offset = offset
	pos.Filename, pos.Line, pos.Column = f.unpack(index(offset), adjusted)
	return
}

// PositionFor returns the Position value for the given file position p.
// If p is out of bounds, it is adjusted to match the File.Offset behavior.
// If adjusted is set, the position may be adjusted by position-altering
// //line comments; otherwise those comments are ignored.
// p must be a Pos value in f or NoPos.
func (f *File) PositionFor(p Pos, adjusted bool) (pos Position) {
	if p != NoPos {
		pos = f.position(p, adjusted)
	}
	return
}

// Position returns the Position value for the given file position p.
// If p is out of bounds, it is adjusted to match the File.Offset behavior.
// Calling f.Position(p) is equivalent to calling f.PositionFor(p, true).
func (f *File) Position(p Pos) (pos Position) {
	return f.PositionFor(p, true)
}

// -----------------------------------------------------------------------------
// Helper functions

func searchInts(a []index, x index) int {
	// This function body is a manually inlined version of:
	//
	//   return sort.Search(len(a), func(i int) bool { return a[i] > x }) - 1
	//
	// With better compiler optimizations, this may not be needed in the
	// future, but at the moment this change improves the go/printer
	// benchmark performance by ~30%. This has a direct impact on the
	// speed of gofmt and thus seems worthwhile (2011-04-29).
	// TODO(gri): Remove this when compilers have caught up.
	i, j := 0, len(a)
	for i < j {
		h := i + (j-i)/2 // avoid overflow when computing h
		// i ≤ h < j
		if a[h] <= x {
			i = h + 1
		} else {
			j = h
		}
	}
	return i - 1
}
