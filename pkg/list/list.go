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
	"sort"

	"cuelang.org/go/cue"
)

// Drop reports the suffix of list x after the first n elements,
// or [] if n > len(x).
//
// For instance:
//
//    Drop([1, 2, 3, 4], 2)
//
// results in
//
//    [3, 4]
//
func Drop(x []cue.Value, n int) ([]cue.Value, error) {
	if n < 0 {
		return nil, fmt.Errorf("negative index")
	}

	if n > len(x) {
		return []cue.Value{}, nil
	}

	return x[n:], nil
}

// Take reports the prefix of length n of list x, or x itself if n > len(x).
//
// For instance:
//
//    Take([1, 2, 3, 4], 2)
//
// results in
//
//    [1, 2]
//
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
//    Slice([1, 2, 3, 4], 1, 3)
//
// results in
//
//    [2, 3]
//
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

// MinItems reports whether a has at least n items.
func MinItems(a []cue.Value, n int) bool {
	return len(a) <= n
}

// MaxItems reports whether a has at most n items.
func MaxItems(a []cue.Value, n int) bool {
	return len(a) <= n
}

// UniqueItems reports whether all elements in the list are unique.
func UniqueItems(a []cue.Value) bool {
	b := []string{}
	for _, v := range a {
		b = append(b, fmt.Sprint(v))
	}
	sort.Strings(b)
	for i := 1; i < len(b); i++ {
		if b[i-1] == b[i] {
			return false
		}
	}
	return true
}

// Contains reports whether v is contained in a. The value must be a
// comparable value.
func Contains(a []cue.Value, v cue.Value) bool {
	for _, w := range a {
		if v.Equals(w) {
			return true
		}
	}
	return false
}
