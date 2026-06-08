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

package pkg

import (
	"fmt"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal/core/adt"
)

type Bottomer interface {
	error
	Bottom() *adt.Bottom
}

type callError struct {
	b *adt.Bottom
}

func (e *callError) Error() string {
	return fmt.Sprint(e.b)
}

// setCallError records b as the call result, dropping the path it reports
// about itself. Call errors are re-wrapped by the call's own context error,
// which already locates them; a path here would duplicate it.
func (c *CallCtxt) setCallError(b *adt.Bottom) {
	c.Err = &callError{b.WithoutPath()}
}

func (c *CallCtxt) errf(underlying error, format string, args ...interface{}) {
	var errs errors.Error
	var code adt.ErrorCode
	switch x := underlying.(type) {
	case nil:
	case Bottomer:
		b := x.Bottom()
		errs = b.Err
		code = b.Code
	case errors.Error:
		errs = x
	case error:
		errs = errors.Promote(x, "")
	}
	// vErr only adds the builtin's message and position; the result is
	// re-wrapped by the call's context error, which locates it. Drop the path
	// here to avoid duplicating that location.
	vErr := c.ctx.NewPosf(c.ctx.Pos(), format, args...).WithoutPath()
	c.Err = &callError{&adt.Bottom{Code: code, Err: errors.Wrap(vErr, errs)}}
}

func (c *CallCtxt) errcf(code adt.ErrorCode, format string, args ...interface{}) {
	err := c.ctx.NewErrf(format, args...)
	err.Code = code
	c.setCallError(err)
}

func (c *CallCtxt) invalidArgType(arg adt.Value, i int, typ string, err error) {
	if ve, ok := err.(Bottomer); ok && ve.Bottom().IsIncomplete() {
		c.Err = ve
		return
	}
	if b, ok := adt.Unwrap(arg).(*adt.Bottom); ok {
		c.Err = b
		return
	}
	// TODO: make these permanent errors if the value did not originate from
	// a reference.
	if err != nil {
		c.errf(err,
			"cannot use %s (type %s) as %s in argument %d",
			arg, arg.Kind(), typ, i)
	} else {
		c.errf(err,
			"cannot use %s (type %s) as %s in argument %d",
			arg, arg.Kind(), typ, i)
	}
}
