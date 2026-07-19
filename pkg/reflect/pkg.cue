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

// This file is maintained by hand: it is the authoritative description
// of the package's API for tooling such as editor completion and hover.
// Its consistency with the registered builtins is checked by the tests
// in cuelang.org/go/pkg.

@experiment(functions)
@pure()

// Package reflect provides functions for inspecting CUE values beyond their
// data, such as the constraints that apply to a field whether or not it is
// present.
package reflect

// Allows reports whether the field or index label may ever appear on the
// struct or list s: an open struct admits any label, a closed struct admits
// its declared fields and labels matching its pattern constraints, and a
// closed list admits in-range indices. It is the structural counterpart to
// the exists builtin, and never errors where exists does: for an undeclared
// name on a closed struct, exists reports an error while Allows reports
// false. An index that no list can have, such as a negative one, is never
// allowed.
//
// Allows is an ordinary function, so s is evaluated as any argument is. A
// disjunction resolves to its default first: for s: *#A | #B, Allows asks
// about #A. A disjunction without a default cannot be resolved, so the
// call stays incomplete, even when every disjunct would give the same
// answer.
Allows: func(s: _, label: _) -> bool

// Field reports the value that the field name has, or would have, in the
// struct s. It is defined as the value of name in the unification
// s & {(name): _}.
//
// For a present regular field this is its value. For a declared optional or
// required field it is the field's constraint, with any applicable pattern
// constraints unified in. For an undeclared field on an open struct it is
// the meet of the applicable pattern constraints, or _ when there are none.
// A field that can never exist — an undeclared name on a closed struct or
// definition — is an error.
Field: func(s: _, name: string) -> _
