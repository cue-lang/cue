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
package errors // import "cuelang.org/go/cue/errors"

import (
	"io"
	"sort"
	"strings"

	"cuelang.org/go/cue/token"
	"github.com/mpvl/unique"
	"golang.org/x/exp/errors"
	"golang.org/x/exp/errors/fmt"
	"golang.org/x/xerrors"
)

// New is a convenience wrapper for errors.New in the core library.
func New(msg string) error {
	return errors.New(msg)
}

// A Message implements the error interface as well as Message to allow
// internationalized messages.
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

var _ Error = &posError{}

// In an List, an error is represented by an *posError.
// The position Pos, if valid, points to the beginning of
// the offending token, and the error condition is described
// by Msg.
type posError struct {
	pos token.Pos
	Message

	// The underlying error that triggered this one, if any.
	err error
}

func (p *posError) Path() []string {
	return Path(p.err)
}

func (p *posError) InputPositions() []token.Pos { return nil }

// E creates a new error.
func E(args ...interface{}) error {
	e := &posError{}
	update(e, args)
	return e
}

func update(e *posError, args []interface{}) {
	err := e.err
	for _, a := range args {
		switch x := a.(type) {
		case string:
			e.Message = NewMessage("%s", []interface{}{x})
		case token.Pos:
			e.pos = x
		case []token.Pos:
			// TODO: do something more clever
			if len(x) > 0 {
				e.pos = x[0]
			}
		case *posError:
			copy := *x
			err = &copy
			e.err = combine(e.err, err)
		case error:
			e.err = combine(e.err, x)
		}
	}
}

func combine(a, b error) error {
	switch x := a.(type) {
	case nil:
		return b
	case List:
		x.add(toErr(b))
		return x
	default:
		return List{toErr(a), toErr(b)}
	}
}

func toErr(err error) Error {
	if e, ok := err.(Error); ok {
		return e
	}
	return &posError{err: err}
}

func (e *posError) Position() token.Pos {
	return e.pos
}

// Error implements the error interface.
func (e *posError) Error() string { return fmt.Sprint(e) }

func (e *posError) FormatError(p errors.Printer) error {
	next := e.err
	if format, args := e.Msg(); format == "" {
		next = errFormat(p, e.err)
	} else {
		p.Printf(format, args...)
	}
	if p.Detail() && e.pos.Filename() != "" || e.pos.IsValid() {
		p.Printf("%s", e.pos.String())
	}
	return next

}

func (e posError) Unwrap() error {
	return e.err
}

func errFormat(p errors.Printer, err error) (next error) {
	switch v := err.(type) {
	case errors.Formatter:
		err = v.FormatError(p)
	default:
		p.Print(err)
		err = nil
	}

	return err
}

// List is a list of *posError.
// The zero value for an List is an empty List ready to use.
//
type List []Error

func (p *List) add(err Error) {
	*p = append(*p, err)
}

// AddNew adds an Error with given position and error message to an List.
func (p *List) AddNew(pos token.Pos, msg string) {
	p.add(&posError{pos: pos, Message: Message{format: msg}})
}

// Add adds an Error with given position and error message to an List.
func (p *List) Add(err error) {
	p.add(toErr(err))
}

// Reset resets an List to no errors.
func (p *List) Reset() { *p = (*p)[0:0] }

// List implements the sort Interface.
func (p List) Len() int      { return len(p) }
func (p List) Swap(i, j int) { p[i], p[j] = p[j], p[i] }

func (p List) Less(i, j int) bool {
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

// Sort sorts an List. *posError entries are sorted by position,
// other errors are sorted by error message, and before any *posError
// entry.
//
func (p List) Sort() {
	sort.Sort(p)
}

// RemoveMultiples sorts an List and removes all but the first error per line.
func (p *List) RemoveMultiples() {
	sort.Sort(p)
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
func (p List) Error() string {
	switch len(p) {
	case 0:
		return "no errors"
	case 1:
		return p[0].Error()
	}
	return fmt.Sprintf("%s (and %d more errors)", p[0], len(p)-1)
}

// Err returns an error equivalent to this error list.
// If the list is empty, Err returns nil.
func (p List) Err() error {
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
}

// Print is a utility function that prints a list of errors to w,
// one error per line, if the err parameter is an List. Otherwise
// it prints the err string.
//
func Print(w io.Writer, err error, cfg *Config) {
	if cfg == nil {
		cfg = &Config{}
	}
	if list, ok := err.(List); ok {
		for _, e := range list {
			printError(w, e, cfg)
		}
	} else if err != nil {
		printError(w, err, cfg)
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
		positions = append(positions, p.String())
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
