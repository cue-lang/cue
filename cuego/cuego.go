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

package cuego

import (
	"fmt"
	"reflect"
	"sync"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
)

// DefaultContext is the shared context used with top-level functions.
var DefaultContext = &Context{}

// MustConstrain is like Constrain, but panics if there is an error.
func MustConstrain(x interface{}, constraints string) {
	if err := Constrain(x, constraints); err != nil {
		panic(err)
	}
}

// Constrain associates the given CUE constraints with the type of x or
// reports an error if the constraints are invalid or not compatible with x.
//
// Constrain works across package boundaries and is typically called in the
// package defining the type. Use a Context to apply constraints locally.
func Constrain(x interface{}, constraints string) error {
	return DefaultContext.Constrain(x, constraints)
}

// Validate is a wrapper for Validate called on the global context.
func Validate(x interface{}) error {
	return DefaultContext.Validate(x)
}

// Complete sets previously undefined values in x that can be uniquely
// determined form the constraints defined on the type of x such that validation
// passes, or returns an error, without modifying anything, if this is not
// possible.
//
// Complete does a JSON round trip. This means that data not preserved in such a
// round trip, such as the location name of a time.Time, is lost after a
// successful update.
func Complete(x interface{}) error {
	return DefaultContext.Complete(x)
}

// A Context holds type constraints that are only applied within a given
// context.
// Global constraints that are defined at the time a constraint is
// created are applied as well.
type Context struct {
	typeCache sync.Map // map[reflect.Type]cue.Value
}

// Validate checks whether x validates against the registered constraints for
// the type of x.
//
// Constraints for x can be defined as field tags or through the Register
// function.
func (c *Context) Validate(x interface{}) error {
	a := c.load(x)
	v, err := fromGoValue(x)
	if err != nil {
		return err
	}
	v = a.Unify(v)
	if err := v.Validate(); err != nil {
		return err
	}
	// TODO: validate all values are concrete. (original value subsumes result?)
	return nil
}

// Complete sets previously undefined values in x that can be uniquely
// determined form the constraints defined on the type of x such that validation
// passes, or returns an error, without modifying anything, if this is not
// possible.
//
// A value is considered undefined if it is pointer type and is nil or if it
// is a field with a zero value and a json tag with the omitempty tag.
// Complete does a JSON round trip. This means that data not preserved in such a
// round trip, such as the location name of a time.Time, is lost after a
// successful update.
func (c *Context) Complete(x interface{}) error {
	a := c.load(x)
	v, err := fromGoValue(x)
	if err != nil {
		return err
	}
	v = a.Unify(v)
	if err := v.Validate(); err != nil {
		return err
	}
	return v.Decode(x)
}

func (c *Context) load(x interface{}) cue.Value {
	t := reflect.TypeOf(x)
	if value, ok := c.typeCache.Load(t); ok {
		return value.(cue.Value)
	}

	// fromGoType should prevent the work is done no more than once, but even
	// if it is, there is no harm done.
	v := fromGoType(x)
	c.typeCache.Store(t, v)
	return v
}

// TODO: should we require that Constrain be defined on exported,
// named types types only?

// Constrain associates the given CUE constraints with the type of x or reports
// an error if the constraints are invalid or not compatible with x.
func (c *Context) Constrain(x interface{}, constraints string) error {
	c.load(x) // Ensure fromGoType is called outside of lock.

	mutex.Lock()
	defer mutex.Unlock()

	expr, err := parser.ParseExpr(fset, fmt.Sprintf("<%T>", x), constraints)
	if err != nil {
		return err
	}

	v := instance.Eval(expr)
	if v.Err() != nil {
		return err
	}

	typ := c.load(x)
	v = typ.Unify(v)

	if err := v.Validate(); err != nil {
		return err
	}

	t := reflect.TypeOf(x)
	c.typeCache.Store(t, v)
	return nil
}

var (
	mutex    sync.Mutex
	instance *cue.Instance
	fset     *token.FileSet
)

func init() {
	context := build.NewContext()
	fset = context.FileSet()
	inst := context.NewInstance("<cuego>", nil)
	if err := inst.AddFile("<cuego>", "{}"); err != nil {
		panic(err)
	}
	instance = cue.Build([]*build.Instance{inst})[0]
	if err := instance.Err; err != nil {
		panic(err)
	}
}

// fromGoValue converts a Go value to CUE
func fromGoValue(x interface{}) (v cue.Value, err error) {
	// TODO: remove the need to have a lock here. We could use a new index (new
	// Instance) here as any previously unrecognized field can never match an
	// existing one and can only be merged.
	mutex.Lock()
	v = internal.FromGoValue(instance, x).(cue.Value)
	mutex.Unlock()
	return v, nil

	// // This should be equivalent to the following:
	// b, err := json.Marshal(x)
	// if err != nil {
	// 	return v, err
	// }
	// expr, err := parser.ParseExpr(fset, "", b)
	// if err != nil {
	// 	return v, err
	// }
	// mutex.Lock()
	// v = instance.Eval(expr)
	// mutex.Unlock()
	// return v, nil

}

func fromGoType(x interface{}) cue.Value {
	// TODO: remove the need to have a lock here. We could use a new index (new
	// Instance) here as any previously unrecognized field can never match an
	// existing one and can only be merged.
	mutex.Lock()
	v := internal.FromGoType(instance, x).(cue.Value)
	mutex.Unlock()
	return v
}
