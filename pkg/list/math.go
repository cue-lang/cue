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

package list

import (
	"fmt"

	"github.com/cockroachdb/apd/v3"

	"cuelang.org/go/internal/core/adt"
)

// Avg returns the average value of a non empty list xs.
func Avg(xs []*adt.Num) (*adt.Num, error) {
	if len(xs) == 0 {
		return nil, fmt.Errorf("empty list")
	}
	s, err := Sum(xs)
	if err != nil {
		return nil, err
	}
	l := &adt.Num{X: *apd.New(int64(len(xs)), 0), K: adt.IntKind}
	return adt.NumQuo(s, l)
}

// Max returns the maximum value of a non empty list xs.
func Max(xs []*adt.Num) (*adt.Num, error) {
	if len(xs) == 0 {
		return nil, fmt.Errorf("empty list")
	}

	max := xs[0]
	for _, x := range xs[1:] {
		if max.X.Cmp(&x.X) == -1 {
			max = x
		}
	}
	return max, nil
}

// Min returns the minimum value of a non empty list xs.
func Min(xs []*adt.Num) (*adt.Num, error) {
	if len(xs) == 0 {
		return nil, fmt.Errorf("empty list")
	}

	min := xs[0]
	for _, x := range xs[1:] {
		if min.X.Cmp(&x.X) == +1 {
			min = x
		}
	}
	return min, nil
}

// Product returns the product of a non empty list xs.
func Product(xs []*adt.Num) (*adt.Num, error) {
	d := &adt.Num{X: *apd.New(1, 0), K: adt.IntKind}
	for _, x := range xs {
		var err error
		if d, err = adt.NumMul(d, x); err != nil {
			return nil, err
		}
	}
	return d, nil
}

// Range generates a list of numbers using a start value, a limit value, and a
// step value.
//
// For instance:
//
//	Range(0, 5, 2)
//
// results in
//
//	[0, 2, 4]
func Range(start, limit, step *adt.Num) ([]*adt.Num, error) {
	if step.X.IsZero() {
		return nil, fmt.Errorf("step must be non zero")
	}

	if !step.X.Negative && start.X.Cmp(&limit.X) == +1 {
		return nil, fmt.Errorf("end must be greater than start when step is positive")
	}

	if step.X.Negative && start.X.Cmp(&limit.X) == -1 {
		return nil, fmt.Errorf("end must be less than start when step is negative")
	}

	var vals []*adt.Num
	num := start
	for {
		if !step.X.Negative && num.X.Cmp(&limit.X) != -1 {
			break
		}

		if step.X.Negative && num.X.Cmp(&limit.X) != +1 {
			break
		}

		vals = append(vals, num)
		next, err := adt.NumAdd(num, step)
		if err != nil {
			return nil, err
		}
		// A step which rounds away beside the value it is added to leaves the
		// range where it started, so stop rather than never advance.
		if next.X.Cmp(&num.X) == 0 {
			return nil, fmt.Errorf("step %v is too small to advance from %v", &step.X, &num.X)
		}
		num = next
	}
	return vals, nil
}

// Sum returns the sum of a list non empty xs.
func Sum(xs []*adt.Num) (*adt.Num, error) {
	d := &adt.Num{K: adt.IntKind}
	for _, x := range xs {
		var err error
		if d, err = adt.NumAdd(d, x); err != nil {
			return nil, err
		}
	}
	return d, nil
}
