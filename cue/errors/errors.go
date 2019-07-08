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

// Package errors defines shared types for handling CUE errors.
//
// The pivotal error type in CUE packages is the interface type Error.
// The information available in such errors can be most easily retrieved using
// the Path, Positions, and Print functions.
package errors // import "cuelang.org/go/cue/errors"

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"cuelang.org/go/cue/token"
	"github.com/mpvl/unique"
	"golang.org/x/xerrors"
)

// New is a convenience wrapper for errors.New in the core library.
// It does not return a CUE error.
func New(msg string) error {
	return errors.New(msg)
}

// A Message implements the error interface as well as Message to allow
// internationalized messages. A Message is typically used as an embedding
// in a CUE message.
type Message struct {
	format string
	args   []interface{}
}

// NewMessage creates an error message for human consumption. The arguments
// are for later consumption, allowing the message to be localized at a later
// time. The passed argument list should not be modified.
func NewMessage(format string, args []interface{}) Message {
	return Message{format: format, args: args}
}

// Msg returns a printf-style format string and its arguments for human
// consumption.
func (m *Message) Msg() (format string, args []interface{}) {
	return m.format, m.args
}

func (m *Message) Error() string {
	return fmt.Sprintf(m.format, m.args...)
}

// Error is the common error message.
type Error interface {
	// Position returns the primary position of an error. If multiple positions
	// contribute equally, this reflects one of them.
	Position() token.Pos

	// InputPositions reports positions that contributed to an error, including
	// the expressions resulting in the conflict, as well as values that were
	// the input to this expression.
	InputPositions() []token.Pos

	// Error reports the error message without position information.
	Error() string

	// Path returns the path into the data tree where the error occurred.
	// This path may be nil if the error is not associated with such a location.
	Path() []string

	// Msg returns the unformatted error message and its arguments for human
	// consumption.
	Msg() (format string, args []interface{})
}

// Positions returns all positions returned by an error, sorted
// by relevance when possible and with duplicates removed.
func Positions(err error) []token.Pos {
	e := Error(nil)
	if !xerrors.As(err, &e) {
		return nil
	}

	a := make([]token.Pos, 0, 3)

	sortOffset := 0
	pos := e.Position()
	if pos.IsValid() {
		a = append(a, pos)
		sortOffset = 1
	}

	for _, p := range e.InputPositions() {
		if p.IsValid() && p != pos {
			a = append(a, p)
		}
	}

	byPos := byPos(a[sortOffset:])
	sort.Sort(byPos)
	k := unique.ToFront(byPos)
	return a[:k+sortOffset]
}

type byPos []token.Pos

func (s *byPos) Truncate(n int)    { (*s) = (*s)[:n] }
func (s byPos) Len() int           { return len(s) }
func (s byPos) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s byPos) Less(i, j int) bool { return comparePos(s[i], s[j]) == -1 }

// Path returns the path of an Error if err is of that type.
func Path(err error) []string {
	if e := Error(nil); xerrors.As(err, &e) {
		return e.Path()
	}
	return nil
}

// Newf creates an Error with the associated position and message.
func Newf(p token.Pos, format string, args ...interface{}) Error {
	return &posError{
		pos:     p,
		Message: NewMessage(format, args),
	}
}

// Wrapf creates an Error with the associated position and message. The provided
// error is added for inspection context.
func Wrapf(err error, p token.Pos, format string, args ...interface{}) Error {
	return &posError{
		pos:     p,
		Message: NewMessage(format, args),
		err:     err,
	}
}

// Promote converts a regular Go error to an Error if it isn't already one.
func Promote(err error, msg string) Error {
	switch x := err.(type) {
	case Error:
		return x
	default:
		return Wrapf(err, token.NoPos, msg)
	}
}

var _ Error = &posError{}

// In an List, an error is represented by an *posError.
// The position Pos, if valid, points to the beginning of
// the offending token, and the error condition is described
// by Msg.
type posError struct {
	pos    token.Pos
	inputs []token.Pos
	Message

	// The underlying error that triggered this one, if any.
	err error
}

func (e *posError) Path() []string              { return Path(e.err) }
func (e *posError) InputPositions() []token.Pos { return e.inputs }
func (e *posError) Position() token.Pos         { return e.pos }
func (e *posError) Unwrap() error               { return e.err }
func (e *posError) Cause() error                { return e.err }

// Error implements the error interface.
func (e *posError) Error() string {
	if e.err == nil {
		return e.Message.Error()
	}
	if e.Message.format == "" {
		return e.err.Error()
	}
	return fmt.Sprintf("%s: %s", e.Message.Error(), e.err)
}

// Append combines two errors, flattening Lists as necessary.
func Append(a, b Error) Error {
	switch x := a.(type) {
	case nil:
		return b
	case list:
		return appendToList(x, b)
	}
	// Preserve order of errors.
	list := appendToList(nil, a)
	list = appendToList(list, b)
	return list
}

// Errors reports the individual errors associated with an error, which is
// the error itself if there is only one or, if the underlying type is List,
// its individual elements. If the given error is not an Error, it will be
// promoted to one.
func Errors(err error) []Error {
	switch x := err.(type) {
	case nil:
		return nil
	case list:
		return []Error(x)
	case Error:
		return []Error{x}
	default:
		return []Error{Promote(err, "")}
	}
}

func appendToList(a list, err Error) list {
	switch x := err.(type) {
	case nil:
		return a
	case list:
		if a == nil {
			return x
		}
		return append(a, x...)
	default:
		return append(a, err)
	}
}

// list is a list of Errors.
// The zero value for an list is an empty list ready to use.
type list []Error

// AddNewf adds an Error with given position and error message to an List.
func (p *list) AddNewf(pos token.Pos, msg string, args ...interface{}) {
	err := &posError{pos: pos, Message: Message{format: msg, args: args}}
	*p = append(*p, err)
}

// Add adds an Error with given position and error message to an List.
func (p *list) Add(err Error) {
	*p = appendToList(*p, err)
}

// Reset resets an List to no errors.
func (p *list) Reset() { *p = (*p)[0:0] }

// List implements the sort Interface.
func (p list) Len() int      { return len(p) }
func (p list) Swap(i, j int) { p[i], p[j] = p[j], p[i] }

func (p list) Less(i, j int) bool {
	if c := comparePos(p[i].Position(), p[j].Position()); c != 0 {
		return c == -1
	}
	// Note that it is not sufficient to simply compare file offsets because
	// the offsets do not reflect modified line information (through //line
	// comments).

	if !equalPath(p[i].Path(), p[j].Path()) {
		return lessPath(p[i].Path(), p[j].Path())
	}
	return p[i].Error() < p[j].Error()
}

func lessOrMore(isLess bool) int {
	if isLess {
		return -1
	}
	return 1
}

func comparePos(a, b token.Pos) int {
	if a.Filename() != b.Filename() {
		return lessOrMore(a.Filename() < b.Filename())
	}
	if a.Line() != b.Line() {
		return lessOrMore(a.Line() < b.Line())
	}
	if a.Column() != b.Column() {
		return lessOrMore(a.Column() < b.Column())
	}
	return 0
}

func lessPath(a, b []string) bool {
	for i, x := range a {
		if i >= len(b) {
			return false
		}
		if x != b[i] {
			return x < b[i]
		}
	}
	return len(a) < len(b)
}

func equalPath(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i, x := range a {
		if x != b[i] {
			return false
		}
	}
	return true
}

// Sanitize sorts multiple errors and removes duplicates on a best effort basis.
// If err represents a single or no error, it returns the error as is.
func Sanitize(err Error) Error {
	if l, ok := err.(list); ok && err != nil {
		a := make(list, len(l))
		copy(a, l)
		a.Sort()
		a.RemoveMultiples()
		return a
	}
	return err
}

// Sort sorts an List. *posError entries are sorted by position,
// other errors are sorted by error message, and before any *posError
// entry.
//
func (p list) Sort() {
	sort.Sort(p)
}

// RemoveMultiples sorts an List and removes all but the first error per line.
func (p *list) RemoveMultiples() {
	p.Sort()
	var last Error
	i := 0
	for _, e := range *p {
		pos := e.Position()
		if last == nil ||
			pos.Filename() != last.Position().Filename() ||
			pos.Line() != last.Position().Line() ||
			!equalPath(e.Path(), last.Path()) {
			last = e
			(*p)[i] = e
			i++
		}
	}
	(*p) = (*p)[0:i]
}

// An List implements the error interface.
func (p list) Error() string {
	format, args := p.Msg()
	return fmt.Sprintf(format, args...)
}

// Msg reports the unformatted error message for the first error, if any.
func (p list) Msg() (format string, args []interface{}) {
	switch len(p) {
	case 0:
		return "no errors", nil
	case 1:
		return p[0].Msg()
	}
	format, args = p[0].Msg()
	return "%s (and %d more errors)", append(args, len(p)-1)
}

// Position reports the primary position for the first error, if any.
func (p list) Position() token.Pos {
	if len(p) == 0 {
		return token.NoPos
	}
	return p[0].Position()
}

// InputPositions reports the input positions for the first error, if any.
func (p list) InputPositions() []token.Pos {
	if len(p) == 0 {
		return nil
	}
	return p[0].InputPositions()
}

// Path reports the path location of the first error, if any.
func (p list) Path() []string {
	if len(p) == 0 {
		return nil
	}
	return p[0].Path()
}

// Err returns an error equivalent to this error list.
// If the list is empty, Err returns nil.
func (p list) Err() error {
	if len(p) == 0 {
		return nil
	}
	return p
}

// A Config defines parameters for printing.
type Config struct {
	// Format formats the given string and arguments and writes it to w.
	// It is used for all printing.
	Format func(w io.Writer, format string, args ...interface{})

	// Cwd is the current working directory. Filename positions are taken
	// relative to this path.
	Cwd string

	// ToSlash sets whether to use Unix paths. Mostly used for testing.
	ToSlash bool
}

// Print is a utility function that prints a list of errors to w,
// one error per line, if the err parameter is an List. Otherwise
// it prints the err string.
//
func Print(w io.Writer, err error, cfg *Config) {
	if cfg == nil {
		cfg = &Config{}
	}
	for _, e := range Errors(err) {
		printError(w, e, cfg)
	}
}

func defaultFprintf(w io.Writer, format string, args ...interface{}) {
	fmt.Fprintf(w, format, args...)
}

func printError(w io.Writer, err error, cfg *Config) {
	if err == nil {
		return
	}
	fprintf := cfg.Format
	if fprintf == nil {
		fprintf = defaultFprintf
	}

	positions := []string{}
	for _, p := range Positions(err) {
		pos := p.Position()
		s := pos.Filename
		if cfg.Cwd != "" {
			if p, err := filepath.Rel(cfg.Cwd, s); err == nil {
				s = p
				// Some IDEs (e.g. VSCode) only recognize a path if it start
				// with a dot. This also helps to distinguish between local
				// files and builtin packages.
				if !strings.HasPrefix(s, ".") {
					s = fmt.Sprintf(".%s%s", string(filepath.Separator), s)
				}
			}
		}
		if cfg.ToSlash {
			s = filepath.ToSlash(s)
		}
		if pos.IsValid() {
			if s != "" {
				s += ":"
			}
			s += fmt.Sprintf("%d:%d", pos.Line, pos.Column)
		}
		if s == "" {
			s = "-"
		}
		positions = append(positions, s)
	}

	if path := Path(err); path != nil {
		fprintf(w, "%s:", strings.Join(path, "."))
	}

	if len(positions) == 0 {
		fprintf(w, "%v\n", err)
		return
	}

	fprintf(w, "%v:\n", err)
	for _, pos := range positions {
		fprintf(w, "    %s\n", pos)
	}
}
