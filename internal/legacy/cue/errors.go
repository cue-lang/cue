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
	"fmt"
	"reflect"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/adt"
)

func (v Value) appendErr(err errors.Error, b *bottom) errors.Error {
	return &valueError{
		v: v,
		err: &adt.Bottom{
			Err: errors.Append(err, b.Err),
		},
	}
}

func (v Value) toErr(b *bottom) (err errors.Error) {
	return &valueError{v: v, err: b}
}

var _ errors.Error = &valueError{}

// A valueError is returned as a result of evaluating a value.
type valueError struct {
	v   Value
	err *bottom
}

func (e *valueError) Error() string {
	return errors.String(e)
}

func (e *valueError) Position() token.Pos {
	src := e.err.Source()
	if src == nil {
		return token.NoPos
	}
	return src.Pos()
}

func (e *valueError) InputPositions() []token.Pos {
	if e.err.Err == nil {
		return nil
	}
	return e.err.Err.InputPositions()
}

func (e *valueError) Msg() (string, []interface{}) {
	if e.err.Err == nil {
		return "", nil
	}
	return e.err.Err.Msg()
}

func (e *valueError) Path() (a []string) {
	return e.v.appendPath(nil)
}

type errCode = adt.ErrorCode

const (
	codeNone       errCode = 0
	codeFatal              = adt.EvalError
	codeNotExist           = adt.NotExistError
	codeTypeError          = adt.EvalError
	codeIncomplete         = adt.IncompleteError
	codeUser               = adt.UserError
	codeCycle              = adt.CycleError
)

func isIncomplete(v value) bool {
	if err, ok := v.(*bottom); ok {
		return err.Code == codeIncomplete || err.Code == codeCycle
	}
	return false
}

func isLiteralBottom(v value) bool {
	if err, ok := v.(*bottom); ok {
		return err.Code == codeUser
	}
	return false
}

var errNotExists = &adt.Bottom{
	Code: codeNotExist,
	Err:  errors.Newf(token.NoPos, "undefined value"),
}

func exists(v value) bool {
	if err, ok := v.(*bottom); ok {
		return err.Code != codeNotExist
	}
	return true
}

func (idx *index) mkErr(src source, args ...interface{}) *bottom {
	var e *adt.Bottom
	var code errCode = -1
outer:
	for i, a := range args {
		switch x := a.(type) {
		case errCode:
			code = x
		case *bottom:
			e = adt.CombineErrors(nil, e, x)
		case []*bottom:
			for _, b := range x {
				e = adt.CombineErrors(nil, e, b)
			}
		case errors.Error:
			e = adt.CombineErrors(nil, e, &adt.Bottom{Err: x})
		case value:
		case string:
			args := args[i+1:]
			// Do not expand message so that errors can be localized.
			pos := pos(src)
			if code < 0 {
				code = 0
			}
			e = adt.CombineErrors(nil, e, &adt.Bottom{
				Code: code,
				Err:  errors.Newf(pos, x, args...),
			})
			break outer
		}
	}
	if code >= 0 {
		e.Code = code
	}
	return e
}

func fixArg(idx *index, x interface{}) interface{} {
	switch x.(type) {
	case uint, int, string:
		return x
	case value:
		return x
	}
	t := reflect.TypeOf(x)
	// Store all non-ptr types as is, as they cannot change.
	if k := t.Kind(); k == reflect.String || k <= reflect.Complex128 {
		return x
	}
	return fmt.Sprint(x)
}

func isBottom(x adt.Node) bool {
	if x == nil {
		return true
	}
	b, _ := x.(*adt.Bottom)
	return b != nil
}

func firstBottom(v ...value) *bottom {
	for _, b := range v {
		if isBottom(b) {
			return b.(*bottom)
		}
	}
	return nil
}
