// Copyright 2025 CUE Authors
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

package rangeset

import (
	"cmp"
	"slices"
)

type FilenameRangeSet struct {
	pairs []filenameRangeSetPair
}

func NewFilenameRangeSet() *FilenameRangeSet {
	return &FilenameRangeSet{}
}

type filenameRangeSetPair struct {
	filename string
	ranges   *RangeSet
}

func (frs *FilenameRangeSet) Add(filename string, start, end int) {
	pairs := frs.pairs
	i, found := slices.BinarySearchFunc(pairs, filename, filenameRangeSetPairCmp)

	if found {
		pairs[i].ranges.Add(start, end)

	} else {
		pair := filenameRangeSetPair{
			filename: filename,
			ranges:   NewRangeSet(),
		}
		pair.ranges.Add(start, end)

		suffix := pairs[i:]
		pairs = pairs[:i]
		pairs = slices.Grow(pairs, 1+len(suffix))
		pairs = pairs[:i+1+len(suffix)]
		copy(pairs[i+1:], suffix)
		pairs[i] = pair
		frs.pairs = pairs
	}
}

func (frs *FilenameRangeSet) Contains(filename string, offset int) bool {
	pairs := frs.pairs
	i, found := slices.BinarySearchFunc(pairs, filename, filenameRangeSetPairCmp)
	return found && pairs[i].ranges.Contains(offset)
}

func filenameRangeSetPairCmp(pair filenameRangeSetPair, filename string) int {
	return cmp.Compare(pair.filename, filename)
}
