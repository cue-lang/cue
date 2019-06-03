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

	"cuelang.org/go/cue/token"
	"golang.org/x/exp/errors"
	"golang.org/x/exp/errors/fmt"
	"golang.org/x/xerrors"
)

// New is a convenience wrapper for errors.New in the core library.
func New(msg string) error {
	return errors.New(msg)
}

// A Handler is a generic error handler used throughout CUE packages.
//
// The position points to the beginning of the offending value.
type Handler func(pos token.Position, msg string)

// Error is the common error message.
type Error interface {
	Position() token.Position

	// Error reports the error message without position information.
	Error() string
}

// // TODO: make Error an interface that returns a list of positions.

// In an List, an error is represented by an *posError.
// The position Pos, if valid, points to the beginning of
// the offending token, and the error condition is described
// by Msg.
type posError struct {
	pos token.Position
	msg string

	// The underlying error that triggered this one, if any.
	err error
}

// E creates a new error.
func E(args ...interface{}) error {
	e := &posError{}
	update(e, args)
	return e
}

// Augment adorns an existing error with new information.
func Augment(err error, args ...interface{}) error {
	e, ok := err.(*posError)
	if !ok {
		e = &posError{err: err}
	}
	update(e, args)
	return e
}

func update(e *posError, args []interface{}) {
	err := e.err
	for _, a := range args {
		switch x := a.(type) {
		case string:
			e.msg = x
		case token.Position:
			e.pos = x
		case []token.Position:
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

func (e *posError) Position() token.Position {
	return e.pos
}

// Error implements the error interface.
func (e *posError) Error() string { return fmt.Sprint(e) }

func (e *posError) FormatError(p errors.Printer) error {
	next := e.err
	if e.msg == "" {
		next = errFormat(p, e.err)
	} else {
		p.Print(e.msg)
	}
	if p.Detail() && e.pos.Filename != "" || e.pos.IsValid() {
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
func (p *List) AddNew(pos token.Position, msg string) {
	p.add(&posError{pos: pos, msg: msg})
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
	e := p[i].Position()
	f := p[j].Position()
	// Note that it is not sufficient to simply compare file offsets because
	// the offsets do not reflect modified line information (through //line
	// comments).
	if e.Filename != f.Filename {
		return e.Filename < f.Filename
	}
	if e.Line != f.Line {
		return e.Line < f.Line
	}
	if e.Column != f.Column {
		return e.Column < f.Column
	}
	return p[i].Error() < p[j].Error()
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
	var last token.Position // initial last.Line is != any legal error line
	i := 0
	for _, e := range *p {
		pos := e.Position()
		if pos.Filename != last.Filename || pos.Line != last.Line {
			last = pos
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

// Print is a utility function that prints a list of errors to w,
// one error per line, if the err parameter is an List. Otherwise
// it prints the err string.
//
func Print(w io.Writer, err error) {
	if list, ok := err.(List); ok {
		for _, e := range list {
			printError(w, e)
		}
	} else if err != nil {
		printError(w, toErr(err))
	}
}

func printError(w io.Writer, err error) {
	fmt.Fprintf(w, "%v", err)
	printedColon := false
	for ; err != nil; err = xerrors.Unwrap(err) {
		switch x := err.(type) {
		case interface{ Position() token.Position }:
			if pos := x.Position().String(); pos != "-" {
				if !printedColon {
					fmt.Fprint(w, ":")
					printedColon = true
				}
				fmt.Fprintf(w, "\n    %v", pos)
			}
		case interface{ Positions() []token.Pos }:
			for _, p := range x.Positions() {
				if p.IsValid() {
					if !printedColon {
						fmt.Fprint(w, ":")
						printedColon = true
					}
					fmt.Fprintf(w, "\n    %v", p)
				}
			}
		}
	}
	fmt.Fprintln(w)
}
