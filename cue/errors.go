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

package cue

import (
	"sort"

	"cuelang.org/go/cue/token"
	"golang.org/x/exp/errors"
	"golang.org/x/exp/errors/fmt"
)

type errCode int

const (
	codeNone errCode = iota
	codeFatal
	codeNotExist
	codeTypeError
	codeIncomplete
	codeCycle
)

func isIncomplete(v value) bool {
	if err, ok := v.(*bottom); ok {
		return err.code == codeIncomplete
	}
	return false
}

func recoverable(v value) bool {
	if err, ok := v.(*bottom); ok {
		switch err.code {
		case codeFatal,
			codeCycle: // only recoverable when explicitly handled and discarded
			return false
		}
	}
	return true
}

var errNotExists = &bottom{code: codeNotExist, msg: "undefined value"}

func exists(v value) bool {
	if err, ok := v.(*bottom); ok {
		return err.code != codeNotExist
	}
	return true
}

// bottom is the bottom of the value lattice. It is subsumed by all values.
type bottom struct {
	baseValue

	index          *index
	code           errCode
	exprDepth      int
	value          value
	offendingValue value
	replacement    evaluated // for cycle resolution
	pos            source
	msg            string

	wrapped *bottom

	// TODO: file at which the error was generated in the code.
	// File positions of where the error occurred.
}

func (x *bottom) kind() kind { return bottomKind }

func (x *bottom) Position() []token.Pos {
	if x.index != nil {
		return appendPositions(nil, x.pos)
	}
	return nil
}

func appendPositions(pos []token.Pos, src source) []token.Pos {
	if src != nil {
		if p := src.Pos(); p != token.NoPos {
			return append(pos, src.Pos())
		}
		if c := src.computed(); c != nil {
			pos = appendPositions(pos, c.x)
			pos = appendPositions(pos, c.y)
		}
	}
	return pos
}

func (x *bottom) Error() string { return fmt.Sprint(x) }

func (x *bottom) FormatError(p errors.Printer) error {
	p.Print(x.msg)
	if p.Detail() && x.index != nil {
		locs := appendLocations(nil, x.pos)
		sort.Strings(locs)
		for _, l := range locs {
			p.Printf("%s\n", l)
		}
	}
	if x.wrapped != nil {
		return x.wrapped // nil interface
	}
	return nil
}

func appendLocations(locs []string, src source) []string {
	if src != nil {
		if p := src.Pos(); p != token.NoPos {
			return append(locs, src.Pos().String())
		}
		if c := src.computed(); c != nil {
			locs = appendLocations(locs, c.x)
			locs = appendLocations(locs, c.y)
		}
	}
	return locs
}

func cycleError(v evaluated) *bottom {
	if err, ok := v.(*bottom); ok && err.code == codeCycle {
		return err
	}
	return nil
}

func (idx *index) mkErrUnify(src source, a, b evaluated) evaluated {
	if err := firstBottom(a, b); err != nil {
		return err
	}
	e := binSrc(src.Pos(), opUnify, a, b)
	// TODO: show string of values and show location of both values.
	return idx.mkErr(e, "incompatible values &(%s, %s)", a.kind(), b.kind())
}

func (c *context) mkIncompatible(src source, op op, a, b evaluated) evaluated {
	if err := firstBottom(a, b); err != nil {
		return err
	}
	e := mkBin(c, src.Pos(), op, a, b)
	return c.mkErr(e, "unsupported op %s(%s, %s)", op, a.kind(), b.kind())
}

func (idx *index) mkErr(src source, args ...interface{}) *bottom {
	e := &bottom{baseValue: src.base(), index: idx, pos: src}

	if v, ok := src.(value); ok {
		e.value = v
	}
	for i, a := range args {
		switch x := a.(type) {
		case errCode:
			e.code = x
		case *bottom:
			e.wrapped = x
			e.offendingValue = x
		case value:
			e.offendingValue = x
		case op:
			panic("no longer using offending value and op")
		case string:
			e.msg += fmt.Sprintf(x, args[i+1:]...)
			return e
		}
	}
	if e.code == codeNone && e.wrapped != nil {
		e.code = e.wrapped.code
	}
	return e
}

func isBottom(n value) bool {
	return n.kind() == bottomKind
}

func firstBottom(v ...value) evaluated {
	for _, b := range v {
		if isBottom(b) {
			return b.(*bottom)
		}
	}
	return nil
}

func expectType(idx *index, t kind, n evaluated) value {
	if isBottom(n) {
		return n
	}
	return idx.mkErr(n, "value should of type %s, found %s", n.kind(), t)
}

// TODO: consider returning a type or subsuption error for op != opUnify
