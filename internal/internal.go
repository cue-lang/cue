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

package internal // import "cuelang.org/go/internal"

// TODO: refactor packages as to make this package unnecessary.

import "github.com/cockroachdb/apd/v2"

// A Decimal is an arbitrary-precision binary-coded decimal number.
//
// Right now Decimal is aliased to apd.Decimal. This may change in the future.
type Decimal = apd.Decimal

// DebugStr prints a syntax node.
var DebugStr func(x interface{}) string

// EvalExpr evaluates an expression within an existing struct value.
// Identifiers only resolve to values defined within the struct.
//
// Expressions may refer to builtin packages if they can be uniquely identified
//
// Both value and result are of type cue.Value, but are an interface to prevent
// cyclic dependencies.
//
// TODO: extract interface
var EvalExpr func(value, expr interface{}) (result interface{})

// FromGoValue converts an arbitrary Go value to the corresponding CUE value.
// instance must be of type *cue.Instance.
// The returned value is a cue.Value, which the caller must cast to.
var FromGoValue func(instance, x interface{}) interface{}

// FromGoType converts an arbitrary Go type to the corresponding CUE value.
// instance must be of type *cue.Instance.
// The returned value is a cue.Value, which the caller must cast to.
var FromGoType func(instance, x interface{}) interface{}

// DropOptional is a blanket override of handling optional values during
// compilation. TODO: should we make this a build option?
var DropOptional bool

// UnifyBuiltin returns the given Value unified with the given builtin template.
var UnifyBuiltin func(v interface{}, kind string) interface{}

// GetRuntime reports the runtime for an Instance.
var GetRuntime func(instance interface{}) interface{}
