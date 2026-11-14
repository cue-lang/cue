// Copyright 2026 CUE Authors
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

// Package reflect provides functions for inspecting CUE values beyond their
// data, such as the constraints that apply to a field whether or not it is
// present.
package reflect

import (
	"fmt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
)

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
func Allows(s cue.Value, label cue.Value) (bool, error) {
	if err := s.Err(); err != nil {
		// A broken value has no kind to report: say what is wrong with it.
		return false, err
	}
	if k := s.IncompleteKind(); k&(cue.StructKind|cue.ListKind) == 0 {
		return false, fmt.Errorf("first argument must be a struct or list, found %s", k)
	}
	var sel cue.Selector
	switch k := label.Kind(); k {
	case cue.StringKind:
		name, err := label.String()
		if err != nil {
			return false, err
		}
		sel = cue.Str(name)
	case cue.IntKind:
		// An index that no list can have, because it is negative or beyond
		// what an index can represent, can never appear: false, not an
		// error.
		i, err := label.Int(nil)
		if err != nil {
			return false, err
		}
		if i.Sign() < 0 || !i.IsInt64() {
			return false, nil
		}
		sel = cue.Index(i.Int64())
		if sel.LabelType() != cue.IndexLabel {
			return false, nil
		}
	default:
		return false, fmt.Errorf("label must be a string or int, found %s", k)
	}
	return s.Allows(sel), nil
}

// Field reports the value that the field name has, or would have, in the
// struct s. It is defined as the value of name in the unification
// s & {(name): _}.
//
// For a present regular field this is its value. For a declared optional or
// required field it is the field's constraint, with any applicable pattern
// constraints unified in. For an undeclared field on an open struct it is
// the meet of the applicable pattern constraints, or _ when there are none:
// such a field could hold anything. A field that can never exist — an
// undeclared name on a closed struct or definition — is an error.
//
// Field makes the constraint of an absent field observable: for example,
// isConcrete(reflect.Field(s, "a")) reports whether the constraint of an
// optional field a is concrete, whereas isConcrete(s.a) reports on the
// member itself and is false while a is absent. Field alone cannot
// distinguish an absent field from a present one whose value is _; combine
// it with exists or allows to disambiguate.
func Field(s cue.Value, name string) (cue.Value, error) {
	if err := s.Err(); err != nil {
		// A broken value has no kind to report: say what is wrong with it.
		return cue.Value{}, err
	}
	if k := s.IncompleteKind(); k&cue.StructKind == 0 {
		return cue.Value{}, fmt.Errorf("first argument must be a struct, found %s", k)
	}
	path := cue.MakePath(cue.Str(name))
	f := s.FillPath(path, ast.NewIdent("_")).LookupPath(path)
	if err := f.Err(); err != nil {
		return cue.Value{}, err
	}
	return f, nil
}
