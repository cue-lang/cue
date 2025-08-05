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
	"fmt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/interpreter/embed"
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

// New creates a new [*cue.Context].
//
// The environment variables CUE_EXPERIMENT and CUE_DEBUG are followed to configure
// the evaluator, just like the cue tool documents via [cue help environment].
// You can override these settings via options like [EvaluatorVersion] and [CUE_DEBUG].
//
// [cue help environment]: https://cuelang.org/docs/reference/command/cue-help-environment/
func New(options ...Option) *cue.Context {
	r := runtime.New()
	// Embedding is always available.
	r.SetInterpreter(embed.New())
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

type EvalVersion = internal.EvaluatorVersion

const (
	// EvalDefault is the default version of the evaluator, which is selected based on
	// the CUE_EXPERIMENT environment variable described in [cue help environment].
	//
	// [cue help environment]: https://cuelang.org/docs/reference/command/cue-help-environment/
	EvalDefault EvalVersion = internal.DefaultVersion

	// EvalDefault is the latest stable version of the evaluator, currently [EvalV3].
	EvalStable EvalVersion = internal.StableVersion

	// EvalExperiment refers to the latest in-development version of the evaluator,
	// currently [EvalV3]. Note that this version may change without notice.
	EvalExperiment EvalVersion = internal.DevVersion

	// EvalV3 is the current version of the evaluator. It was introduced in 2024
	// and brought a new disjunction algorithm, a new closedness algorithm, a
	// new core scheduler, and adds performance enhancements like structure sharing.
	EvalV3 EvalVersion = internal.EvalV3
)

// EvaluatorVersion indicates which version of the evaluator to use. Currently
// only experimental versions can be selected as an alternative.
func EvaluatorVersion(v EvalVersion) Option {
	return Option{func(r *runtime.Runtime) {
		r.SetVersion(v)
	}}
}

// CUE_DEBUG takes a string with the same contents as CUE_DEBUG and configures
// the context with the relevant debug options. It panics for unknown or
// malformed options.
func CUE_DEBUG(s string) Option {
	var c cuedebug.Config
	if err := envflag.Parse(&c, s); err != nil {
		panic(fmt.Errorf("cuecontext.CUE_DEBUG: %v", err))
	}

	return Option{func(r *runtime.Runtime) {
		r.SetDebugOptions(&c)
	}}
}
