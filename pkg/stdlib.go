// Copyright 2026 The CUE Authors
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

import "cuelang.org/go/internal/pkg"

// Each standard library package has a hand-maintained pkg.cue file
// alongside its generated pkg.go file, declaring each of the
// package's members. A function is declared as its signature, a
// function type such as
//
//	Compare: func(a: string, b: string) -> int
//
// where each plain name is both a callable contract label and the name of a
// positional slot. The builtin ABI still receives arguments by position. The
// definition files are authored, so they may say what a Go signature cannot,
// and their consistency with the registered builtins is checked by this
// package's tests: the same members, agreeing arity, parameter labels, and
// defaults. A kind-level
// derivation from the Go signatures is separately unified with the
// builtins in each package's runtime CUE, rejecting a registration
// that drifts from the Go sources.
//
// A builtin intended for use as a validator of its first argument,
// which its Go signature declares by returning
// [cuelang.org/go/internal/pkg.Validator], is declared as a
// disjunction of its two call shapes, the validator form first:
//
//	MinRunes: (func(min: int) -> validator(string)) |
//		(func(s: string, min: int) -> bool)
//
// The validator form is the call form with the validated first
// parameter moved into the result: applying it to the remaining
// arguments yields a validator of that parameter, as in
// strings.MinRunes(3). A builtin whose only parameter is the validated
// value is the bare validator type, as in validator([...]) for
// list.UniqueItems. A parameter that deliberately accepts a
// non-concrete value carries the @schema() attribute.
//
// Constants are declared with their concrete values, and members that
// are defined in CUE appear as their original declarations. Doc
// comments are carried over from the corresponding Go declarations.
//
// These files describe the standard library; they are not its
// implementation and are not loaded during evaluation. They exist for
// tooling that needs to present the standard library without
// evaluating it, such as editor completion and hover documentation.

// Source returns the CUE source of the generated definitions
// describing the API of the standard library package with the given
// import path, such as "strings" or "encoding/json". It reports false
// if there is no such package.
//
// Experimental: this API may change.
func Source(importPath string) ([]byte, bool) {
	src, ok := pkg.Source(importPath)
	if !ok {
		return nil, false
	}
	return []byte(src), true
}

// ImportPaths returns the import paths of all standard library
// packages, sorted lexically.
//
// Experimental: this API may change.
func ImportPaths() []string {
	return pkg.SourceImportPaths()
}
