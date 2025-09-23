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

// Package list contains functions for manipulating and examining lists.
package list

import (
	"fmt"
	"slices"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/iterutil"
	"cuelang.org/go/internal/pkg"
	"cuelang.org/go/internal/types"
	"cuelang.org/go/internal/value"
)

// Drop reports the suffix of list x after the first n elements,
// or [] if n > len(x).
//
// For instance:
//
//	Drop([1, 2, 3, 4], 2)
//
// results in
//
//	[3, 4]
func Drop(x []cue.Value, n int) ([]cue.Value, error) {
	if n < 0 {
		return nil, fmt.Errorf("negative index")
	}

	if n > len(x) {
		return []cue.Value{}, nil
	}

	return x[n:], nil
}

// TODO: disable Flatten until we know the right default for depth.
//       The right time to determine is at least some point after the query
//       extensions are introduced, which may provide flatten functionality
//       natively.
//
// // Flatten reports a flattened sequence of the list xs by expanding any elements
// // that are lists.
// //
// // For instance:
// //
// //    Flatten([1, [[2, 3], []], [4]])
// //
// // results in
// //
// //    [1, 2, 3, 4]
// //
// func Flatten(xs cue.Value) ([]cue.Value, error) {
// 	var flatten func(cue.Value) ([]cue.Value, error)
// 	flatten = func(xs cue.Value) ([]cue.Value, error) {
// 		var res []cue.Value
// 		iter, err := xs.List()
// 		if err != nil {
// 			return nil, err
// 		}
// 		for iter.Next() {
// 			val := iter.Value()
// 			if val.Kind() == cue.ListKind {
// 				vals, err := flatten(val)
// 				if err != nil {
// 					return nil, err
// 				}
// 				res = append(res, vals...)
// 			} else {
// 				res = append(res, val)
// 			}
// 		}
// 		return res, nil
// 	}
// 	return flatten(xs)
// }

// FlattenN reports a flattened sequence of the list xs by expanding any elements
// depth levels deep. If depth is negative all elements are expanded.
//
// For instance:
//
//	FlattenN([1, [[2, 3], []], [4]], 1)
//
// results in
//
//	[1, [2, 3], [], 4]
func FlattenN(xs cue.Value, depth int) ([]cue.Value, error) {
	var flattenN func(cue.Value, int) ([]cue.Value, error)
	flattenN = func(xs cue.Value, depth int) ([]cue.Value, error) {
		var res []cue.Value
		iter, err := xs.List()
		if err != nil {
			return nil, err
		}
		for iter.Next() {
			val, _ := iter.Value().Default()
			if val.Kind() == cue.ListKind && depth != 0 {
				d := depth - 1
				values, err := flattenN(val, d)
				if err != nil {
					return nil, err
				}
				res = append(res, values...)
			} else {
				res = append(res, val)
			}
		}
		return res, nil
	}
	return flattenN(xs, depth)
}

// Repeat returns a new list consisting of count copies of list x.
//
// For instance:
//
//	Repeat([1, 2], 2)
//
// results in
//
//	[1, 2, 1, 2]
func Repeat(x []cue.Value, count int) ([]cue.Value, error) {
	if count < 0 {
		return nil, fmt.Errorf("negative count")
	}
	return slices.Repeat(x, count), nil
}

// Concat takes a list of lists and concatenates them.
//
// Concat([a, b, c]) is equivalent to
//
//	[for x in a {x}, for x in b {x}, for x in c {x}]
func Concat(a []cue.Value) ([]cue.Value, error) {
	var res []cue.Value
	for _, e := range a {
		iter, err := e.List()
		if err != nil {
			return nil, err
		}
		for iter.Next() {
			res = append(res, iter.Value())
		}
	}
	return res, nil
}

// Take reports the prefix of length n of list x, or x itself if n > len(x).
//
// For instance:
//
//	Take([1, 2, 3, 4], 2)
//
// results in
//
//	[1, 2]
func Take(x []cue.Value, n int) ([]cue.Value, error) {
	if n < 0 {
		return nil, fmt.Errorf("negative index")
	}

	if n > len(x) {
		return x, nil
	}

	return x[:n], nil
}

// Slice extracts the consecutive elements from list x starting from position i
// up till, but not including, position j, where 0 <= i < j <= len(x).
//
// For instance:
//
//	Slice([1, 2, 3, 4], 1, 3)
//
// results in
//
//	[2, 3]
func Slice(x []cue.Value, i, j int) ([]cue.Value, error) {
	if i < 0 {
		return nil, fmt.Errorf("negative index")
	}

	if i > j {
		return nil, fmt.Errorf("invalid index: %v > %v", i, j)
	}

	if i > len(x) {
		return nil, fmt.Errorf("slice bounds out of range")
	}

	if j > len(x) {
		return nil, fmt.Errorf("slice bounds out of range")
	}

	return x[i:j], nil
}

// Reverse reverses a list.
//
// For instance:
//
//	Reverse([1, 2, 3, 4])
//
// results in
//
//	[4, 3, 2, 1]
func Reverse(x []cue.Value) []cue.Value {
	slices.Reverse(x)
	return x
}

// MinItems reports whether a has at least n items.
func MinItems(list pkg.List, n int) (bool, error) {
	count := iterutil.Count(list.Elems())
	if count >= n {
		return true, nil
	}
	code := adt.EvalError
	if list.IsOpen() {
		code = adt.IncompleteError
	}
	return false, pkg.ValidationError{B: &adt.Bottom{
		Code: code,
		Err:  errors.Newf(token.NoPos, "len(list) < MinItems(%[2]d) (%[1]d < %[2]d)", count, n),
	}}
}

// MaxItems reports whether a has at most n items.
func MaxItems(list pkg.List, n int) (bool, error) {
	count := iterutil.Count(list.Elems())
	if count > n {
		return false, pkg.ValidationError{B: &adt.Bottom{
			Code: adt.EvalError,
			Err:  errors.Newf(token.NoPos, "len(list) > MaxItems(%[2]d) (%[1]d > %[2]d)", count, n),
		}}
	}

	return true, nil
}

// UniqueItems reports whether all elements in the list are unique.
func UniqueItems(a []cue.Value) (bool, error) {
	if len(a) <= 1 {
		return true, nil
	}

	// TODO(perf): this is an O(n^2) algorithm. We should make it O(n log n).
	// This could be done as follows:
	// - Create a list with some hash value for each element x in a as well
	//   alongside the value of x itself.
	// - Sort the elements based on the hash value.
	// - Compare subsequent elements to see if they are equal.

	var tv types.Value
	a[0].Core(&tv)
	ctx := eval.NewContext(tv.R, tv.V)

	posX, posY := 0, 0
	code := adt.IncompleteError

outer:
	for i, x := range a {
		_, vx := value.ToInternal(x)

		for j := i + 1; j < len(a); j++ {
			_, vy := value.ToInternal(a[j])

			if adt.Equal(ctx, vx, vy, adt.RegularOnly) {
				posX, posY = i, j
				if adt.IsFinal(vy) {
					code = adt.EvalError
					break outer
				}
			}
		}
	}

	if posX == posY {
		return true, nil
	}

	var err errors.Error
	switch x := a[posX].Value(); x.Kind() {
	case cue.BoolKind, cue.NullKind, cue.IntKind, cue.FloatKind, cue.StringKind, cue.BytesKind:
		err = errors.Newf(token.NoPos, "equal value (%v) at position %d and %d", x, posX, posY)
	default:
		err = errors.Newf(token.NoPos, "equal values at position %d and %d", posX, posY)
	}

	return false, pkg.ValidationError{B: &adt.Bottom{
		Code: code,
		Err:  err,
	}}
}

// Contains reports whether v is contained in a. The value must be a
// comparable value.
func Contains(a []cue.Value, v cue.Value) bool {
	return slices.ContainsFunc(a, v.Equals)
}

// MatchN is a validator that checks that the number of elements in the given
// list that unifies with the schema "matchValue" matches "n".
// "n" may be a number constraint and does not have to be a concrete number.
// Likewise, "matchValue" will usually be a non-concrete value.
func MatchN(list []cue.Value, n pkg.Schema, matchValue pkg.Schema) (bool, error) {
	c := value.OpContext(n)
	return matchN(c, list, n, matchValue)
}

// matchN is the actual implementation of MatchN.
func matchN(c *adt.OpContext, list []cue.Value, n pkg.Schema, matchValue pkg.Schema) (bool, error) {
	var nmatch int64
	for _, w := range list {
		vx := adt.Unify(c, value.Vertex(matchValue), value.Vertex(w))
		x := value.Make(c, vx)
		if x.Validate(cue.Final()) == nil {
			nmatch++
		}
	}

	ctx := value.Context(c)

	if err := n.Unify(ctx.Encode(nmatch)).Err(); err != nil {
		return false, pkg.ValidationError{B: &adt.Bottom{
			Code: adt.EvalError,
			Err: errors.Newf(
				token.NoPos,
				"number of matched elements is %d: does not satisfy %v",
				nmatch,
				n,
			),
		}}
	}

	return true, nil
}
