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

package astinternal

import "strconv"

// A UniqueNamer picks names which are not taken. A name must stay taken once
// it is, so that each search can resume where the last one for the same base
// stopped, rather than scanning past every taken name again.
type UniqueNamer struct {
	next map[string]int // the suffix at which to resume the search for a base
}

// Name returns base if it is not taken, or otherwise the first of base_1,
// base_2, and so on which is not.
func (u *UniqueNamer) Name(base string, taken func(name string) bool) string {
	start := u.next[base]
	for i := start; ; i++ {
		name := base
		if i > 0 {
			name += "_" + strconv.Itoa(i)
		}
		if taken(name) {
			continue
		}
		if i > start {
			if u.next == nil {
				u.next = make(map[string]int)
			}
			u.next[base] = i
		}
		return name
	}
}
