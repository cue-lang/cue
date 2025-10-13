// Copyright 2019 CUE Authors
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

// Package task provides a registry for tasks to be used by commands.
package task

import (
	"context"
	"io"
	"sync"
	"sync/atomic"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/value"
	"cuelang.org/go/tools/flow"
)

// A Context provides context for running a task.
type Context struct {
	Context context.Context

	TaskKey func(v cue.Value) (string, error)

	Root   cue.Value
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	Obj    cue.Value
	Err    errors.Error
}

func (c *Context) Lookup(field string) cue.Value {
	f := c.Obj.LookupPath(cue.MakePath(cue.Str(field)))
	if !f.Exists() {
		c.addErr(f, nil, "could not find field %q", field)
		return cue.Value{}
	}
	if err := f.Err(); err != nil {
		c.Err = errors.Append(c.Err, errors.Promote(err, "lookup"))
	}
	return f
}

func (c *Context) Int64(field string) int64 {
	f := c.Obj.LookupPath(cue.MakePath(cue.Str(field)))
	value, err := f.Int64()
	if err != nil {
		c.addErr(f, err, "invalid integer argument")
		return 0
	}
	return value
}

func (c *Context) String(field string) string {
	f := c.Obj.LookupPath(cue.MakePath(cue.Str(field)))
	value, err := f.String()
	if err != nil {
		c.addErr(f, err, "invalid string argument")
		return ""
	}
	return value
}

func (c *Context) Bytes(field string) []byte {
	f := c.Obj.LookupPath(cue.MakePath(cue.Str(field)))
	value, err := f.Bytes()
	if err != nil {
		c.addErr(f, err, "invalid bytes argument")
		return nil
	}
	return value
}

func (c *Context) addErr(v cue.Value, wrap error, format string, args ...interface{}) {

	err := &taskError{
		task:    c.Obj,
		v:       v,
		Message: errors.NewMessagef(format, args...),
	}
	c.Err = errors.Append(c.Err, errors.Wrap(err, wrap))
}

// ErrLegacy is a sentinel error value that may be returned by a TaskKey
// function to indicate that the task is a legacy task. This will cause the
// configuration value to be passed to the RunnerFunc instead of an empty
// value.
var ErrLegacy error = errors.New("legacy task error")

// NewTaskFunc creates a flow.TaskFunc that uses global settings from Context
// and a taskKey function to determine the kind of task to run.
func (c Context) TaskFunc(didWork *atomic.Bool) flow.TaskFunc {
	return func(v cue.Value) (flow.Runner, error) {
		kind, err := c.TaskKey(v)
		var isLegacy bool
		if err == ErrLegacy {
			err = nil
			isLegacy = true
		}
		if err != nil || kind == "" {
			return nil, err
		}

		didWork.Store(true)

		rf := Lookup(kind)
		if rf == nil {
			return nil, errors.Newf(v.Pos(), "runner of kind %q not found", kind)
		}

		// Verify entry against template.
		v = value.UnifyBuiltin(v, kind)
		if err := v.Err(); err != nil {
			err = v.Validate()
			return nil, errors.Promote(err, "newTask")
		}

		runner, err := rf(v)
		if err != nil {
			return nil, errors.Promote(err, "errors running task")
		}

		if !isLegacy {
			v = cue.Value{}
		}

		return c.flowFunc(runner, v), nil
	}
}

// flowFunc takes a Runner and a schema v, which should only be defined for
// legacy task ids.
func (c Context) flowFunc(runner Runner, v cue.Value) flow.RunnerFunc {
	return flow.RunnerFunc(func(t *flow.Task) error {
		// Set task-specific values.
		c.Context = t.Context()
		c.Obj = t.Value()
		if v.Exists() {
			c.Obj = c.Obj.Unify(v)
		}
		value, err := runner.Run(&c)
		if err != nil {
			return err
		}
		if value != nil {
			_ = t.Fill(value)
		}
		return nil
	})
}

// taskError wraps some error values to retain position information about the
// error.
type taskError struct {
	task cue.Value
	v    cue.Value
	errors.Message
}

var _ errors.Error = &taskError{}

func (t *taskError) Path() (a []string) {
	for _, x := range t.v.Path().Selectors() {
		a = append(a, x.String())
	}
	return a
}

func (t *taskError) Position() token.Pos {
	return t.task.Pos()
}

func (t *taskError) InputPositions() (a []token.Pos) {
	_, nx := value.ToInternal(t.v)

	for x := range nx.LeafConjuncts() {
		if src := x.Source(); src != nil {
			a = append(a, src.Pos())
		}
	}
	return a
}

// A RunnerFunc creates a Runner.
type RunnerFunc func(v cue.Value) (Runner, error)

// A Runner defines a command type.
type Runner interface {
	// Init is called with the original configuration before any task is run.
	// As a result, the configuration may be incomplete, but allows some
	// validation before tasks are kicked off.
	// Init(v cue.Value)

	// Runner runs given the current value and returns a new value which is to
	// be unified with the original result.
	Run(ctx *Context) (results interface{}, err error)
}

// Register registers a task for cue commands.
func Register(key string, f RunnerFunc) {
	runners.Store(key, f)
}

// Lookup returns the RunnerFunc for a key.
func Lookup(key string) RunnerFunc {
	v, ok := runners.Load(key)
	if !ok {
		return nil
	}
	return v.(RunnerFunc)
}

var runners sync.Map
