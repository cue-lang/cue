// Copyright 2023 CUE Authors
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

package updater

import "cuelang.org/go/cue"

// VisitFunc is called for each path-value pair in a value.
type VisitFunc func(p cue.Path, v cue.Value)

// VisitPaths calls f for every path-value pair in v. Only regular fields
// are visited.
// The are visited in an pre-order traversal using cue.Value.Fields.
func VisitPaths(v cue.Value, f VisitFunc) {
	var p pathIter
	p.iter(v, f)
}

type pathIter struct {
	path []cue.Selector
}

func (p *pathIter) iter(v cue.Value, f VisitFunc) {
	if v.IncompleteKind()&^(cue.StructKind|cue.ListKind) != 0 {
		f(cue.MakePath(p.path...), v)
	}

	for i, _ := v.Fields(); i.Next(); {
		n := len(p.path)
		p.path = append(p.path, i.Selector())
		p.iter(i.Value(), f)
		p.path = p.path[:n]
	}
}
