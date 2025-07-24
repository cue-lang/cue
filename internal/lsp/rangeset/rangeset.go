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
	"fmt"
	"slices"
	"sort"
)

// Range represents a single continuous interval [Start, End). The
// interval includes Start but excludes End.
type Range struct {
	Start int
	End   int
}

// RangeSet holds a collection of sorted, non-overlapping ranges.
type RangeSet struct {
	ranges []Range
}

// NewRangeSet creates and returns a new, empty RangeSet.
func NewRangeSet() *RangeSet {
	return &RangeSet{}
}

// Add incorporates a new range into the set. It finds all existing
// ranges that overlap or are adjacent to the new range and merges
// them into a single, larger range.
func (rs *RangeSet) Add(start, end int) {
	if start >= end {
		return // Ignore empty or invalid ranges.
	}

	newRange := Range{Start: start, End: end}
	ranges := rs.ranges

	// Find the insertion/merge start point using binary search.
	// i is the index of the first range that we might need to merge
	// with. This is the first range r where r.End >= newRange.Start.
	i := sort.Search(len(ranges), func(k int) bool {
		return ranges[k].End >= newRange.Start
	})

	// Find the insertion/merge end point. j is the index of the first
	// range that starts after our new range.
	j := sort.Search(len(ranges), func(k int) bool {
		return ranges[k].Start > newRange.End
	})

	// Note that sort.Search returns its first argument if the func
	// never returns true. So for example, if there is no range that
	// either starts or ends beyond start then i = j = len(ranges).

	// If i < j, newRange overlaps with ranges[i:j].
	if i < j {
		// Merge the new range with all the overlapping ranges.
		// The merged range starts at the minimum of the starts.
		if ranges[i].Start < newRange.Start {
			newRange.Start = ranges[i].Start
		}
		// And ends at the maximum of the ends.
		if ranges[j-1].End > newRange.End {
			newRange.End = ranges[j-1].End
		}
	}

	// Replace the ranges[i:j] with the single newRange.
	suffix := ranges[j:]
	ranges = ranges[:i]
	ranges = slices.Grow(ranges, 1+len(suffix))
	ranges = ranges[:i+1+len(suffix)]
	copy(ranges[i+1:], suffix)
	ranges[i] = newRange
	rs.ranges = ranges
}

// Contains reports if an offset is within any of the ranges in the
// set.
func (rs *RangeSet) Contains(offset int) bool {
	// Use sort.Search to find the index of the first range that
	// *could* contain the offset. This would be the first range whose
	// end is > offset.
	ranges := rs.ranges
	i := sort.Search(len(ranges), func(k int) bool {
		return ranges[k].End > offset
	})

	if i < len(ranges) {
		return ranges[i].Start <= offset
	}

	return false
}

// Ranges returns the sorted ranges that make up this range set.
func (rs *RangeSet) Ranges() []Range {
	return slices.Clone(rs.ranges)
}

func (rs *RangeSet) String() string {
	return fmt.Sprintf("RangeSet{%v}", rs.ranges)
}
