// Copyright 2021 CUE Authors
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

package cuecontext

import (
	"cuelang.org/go/cue"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/cuedebug"
	"cuelang.org/go/internal/envflag"

	_ "cuelang.org/go/pkg"
)

// Option controls a build context.
type Option struct {
	apply func(r *runtime.Runtime)
}

// New creates a new Context.
func New(options ...Option) *cue.Context {
	r := runtime.New()
	for _, o := range options {
		o.apply(r)
	}
	return (*cue.Context)(r)
}

// An ExternInterpreter creates a compiler that can produce implementations of
// functions written in a language other than CUE. It is currently for internal
// use only.
type ExternInterpreter = runtime.Interpreter

// Interpreter associates an interpreter for external code with this context.
func Interpreter(i ExternInterpreter) Option {
	return Option{func(r *runtime.Runtime) {
		r.SetInterpreter(i)
	}}
}

type EvaluatorVersion = internal.EvaluatorVersion

const (
	// Latest is the latest stable version of the evaluator.
	Latest EvaluatorVersion = V2

	// Experimental refers to the latest unstable version of the evaluator.
	// Note that this version may change without notice.
	Experimental EvaluatorVersion = V3

	// V2 is the currently latest stable version of the evaluator.
	V2 EvaluatorVersion = internal.DefaultVersion

	// V3 is the currently experimental version of the evaluator.
	V3 EvaluatorVersion = internal.DevVersion
)

// Version indicates which version of the evaluator to use. Currently only
// experimental versions can be selected as an alternative.
func Version(v EvaluatorVersion) Option {
	return Option{func(r *runtime.Runtime) {
		r.SetVersion(v)
	}}
}

// CUE_DEBUG takes a string with the same contents as CUE_DEBUG and configures
// the context with the relevant debug options.
func CUE_DEBUG(s string) Option {
	return Option{func(r *runtime.Runtime) {
		var c cuedebug.Config
		envflag.Parse(&c, "CUE_DEBUG", s)
		r.SetDebugOptions(&c)
	}}
}
