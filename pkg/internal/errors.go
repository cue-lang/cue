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

package internal

import (
	"encoding/json"
	"fmt"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
)

type bottomer interface {
	error
	Bottom() *adt.Bottom
}

type callError struct {
	b *adt.Bottom
}

func (e *callError) Error() string {
	return fmt.Sprint(e.b)
}

func (c *CallCtxt) errf(src adt.Node, underlying error, format string, args ...interface{}) {
	var errs errors.Error
	var code adt.ErrorCode
	if err, ok := underlying.(bottomer); ok {
		b := err.Bottom()
		errs = b.Err
		code = b.Code
	}
	errs = errors.Wrapf(errs, c.ctx.Pos(), format, args...)
	c.Err = &callError{&adt.Bottom{Code: code, Err: errs}}
}

func (c *CallCtxt) errcf(src adt.Node, code adt.ErrorCode, format string, args ...interface{}) {
	err := c.ctx.NewErrf(format, args...)
	err.Code = code
	c.Err = &callError{err}
}

func wrapCallErr(c *CallCtxt, b *adt.Bottom) *adt.Bottom {
	pos := token.NoPos
	if c.src != nil {
		if src := c.src.Source(); src != nil {
			pos = src.Pos()
		}
	}
	var err errors.Error
	for _, e := range errors.Errors(b.Err) {
		const msg = "error in call to %s"
		err = errors.Append(err,
			errors.Wrapf(e, pos, msg, c.builtin.name(c.ctx)))
	}
	return &adt.Bottom{Code: b.Code, Err: err}
}

func (c *CallCtxt) convertError(x interface{}, name string) *adt.Bottom {
	var err errors.Error
	switch v := x.(type) {
	case nil:
		return nil

	case *adt.Bottom:
		return v

	case *json.MarshalerError:
		err = errors.Promote(v, "marshal error")

	case errors.Error:
		err = v

	case error:
		if name != "" {
			err = errors.Newf(c.Pos(), "%s: %v", name, v)
		} else {
			err = errors.Newf(c.Pos(), "error in call to %s: %v", c.Name(), v)
		}

	default:
		err = errors.Newf(token.NoPos, "%s", name)
	}
	if err != internal.ErrIncomplete {
		return &adt.Bottom{
			// Wrap to preserve position information.
			Err: errors.Wrapf(err, c.Pos(), "error in call to %s", c.Name()),
		}
	}
	return &adt.Bottom{
		Code: adt.IncompleteError,
		Err:  errors.Newf(c.Pos(), "incomplete values in call to %s", c.Name()),
	}
}

func (c *CallCtxt) invalidArgType(arg adt.Expr, i int, typ string, err error) {
	if ve, ok := err.(bottomer); ok && ve.Bottom().IsIncomplete() {
		c.Err = ve
		return
	}
	v, ok := arg.(adt.Value)
	// TODO: make these permanent errors if the value did not originate from
	// a reference.
	if !ok {
		c.errf(c.src, nil,
			"cannot use incomplete value %s as %s in argument %d to %s: %v",
			c.ctx.Str(arg), typ, i, c.Name(), err)
	}
	if err != nil {
		c.errf(c.src, err,
			"cannot use %s (type %s) as %s in argument %d to %s: %v",
			c.ctx.Str(arg), v.Kind(), typ, i, c.Name(), err)
	} else {
		c.errf(c.src, err,
			"cannot use %s (type %s) as %s in argument %d to %s",
			c.ctx.Str(arg), v.Kind(), typ, i, c.Name())
	}
}
