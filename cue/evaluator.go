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

func (c *context) manifest(v value) evaluated {
	evaluated := v.evalPartial(c)
	if c.noManifest {
		return evaluated
	}
outer:
	for {
		switch x := evaluated.(type) {
		case *disjunction:
			evaluated = x.manifest(c)

		case *list:
			return x.manifest(c)

		default:
			break outer
		}
	}
	return evaluated
}

type evaluator struct {
	ctx    *context
	bottom []*bottom
}

const (
	// (fmt, evaluated, orig, gotKind, wantKind)
	// "invalid [string index] -1 (index must be non-negative)"
	// "invalid operation: %[1]s (type %[3] does not support indexing)"
	// msgType   = "invalid %s %s (must be type %s)"
	msgGround = "invalid non-ground value %[1]s (must be concrete %[4]s)"
)

func newEval(ctx *context, manifest bool) evaluator {
	return evaluator{ctx: ctx}
}

func (e *evaluator) hasErr() bool {
	return len(e.bottom) > 0
}

func (e *evaluator) mkErr(orig, eval value, code errCode, want kind, desc string, args ...interface{}) (err *bottom) {
	args = append([]interface{}{
		eval,
		code,
		desc,        // format string
		eval,        // 1
		orig,        // 2
		eval.kind(), // 3
		want},       // 4
		args...)
	for i := 3; i < len(args); i++ {
		switch v := args[i].(type) {
		case value:
			args[i] = debugStr(e.ctx, v)
		}
	}
	err = e.ctx.mkErr(orig, args...)
	// TODO: maybe replace with more specific type error.
	for i, old := range e.bottom {
		if old == eval {
			e.bottom[i] = err
			return err
		}
	}
	e.bottom = append(e.bottom, err)
	return err
}

func (e *evaluator) eval(v value, want kind, desc string, extraArgs ...interface{}) evaluated {
	eval := e.ctx.manifest(v)

	if isBottom(eval) {
		e.bottom = append(e.bottom, eval.(*bottom))
		return eval
	}
	got := eval.kind()
	if got&want == bottomKind {
		return e.mkErr(v, eval, codeTypeError, want, desc, extraArgs...)
	}
	if !got.isGround() {
		return e.mkErr(v, eval, codeIncomplete, want, msgGround, extraArgs...)
	}
	return eval
}

func (e *evaluator) evalPartial(v value, want kind, desc string, extraArgs ...interface{}) evaluated {
	eval := v.evalPartial(e.ctx)
	if isBottom(eval) {
		// handle incomplete errors separately?
		e.bottom = append(e.bottom, eval.(*bottom))
		return eval
	}
	got := eval.kind()
	if got&want == bottomKind {
		return e.mkErr(v, eval, codeTypeError, want, desc, extraArgs...)
	}
	return eval
}

func (e *evaluator) evalAllowNil(v value, want kind, desc string, extraArgs ...interface{}) evaluated {
	if v == nil {
		return nil
	}
	return e.eval(v, want, desc, extraArgs...)
}

func (e *evaluator) is(v value, want kind, desc string, args ...interface{}) bool {
	if isBottom(v) {
		// Even though errors are ground, we treat them as not allowed.
		return false
	}
	got := v.kind()
	if got&want == bottomKind {
		e.mkErr(v, v, codeTypeError, want, desc, args...)
		return false
	}
	// groundness must already have been checked.
	return true
}

func (e *evaluator) err(v value) evaluated {
	// if bottom is a fatal (not incomplete) error, return that.
	// otherwise, try to extract a fatal error from the given value.
	// otherwise return an incomplete error with the given value as offending.
	for _, b := range e.bottom {
		if b.code != codeIncomplete {
			return b
		}
	}
	b := *e.bottom[0]
	b.code = codeIncomplete
	return &b
}
