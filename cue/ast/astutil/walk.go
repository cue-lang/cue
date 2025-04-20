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

package astutil

import "cuelang.org/go/cue/ast"

// A visitor's before method is invoked for each node encountered by Walk.
// If the result visitor w is true, Walk visits each of the children
// of node with the visitor w, followed by a call of w.After.
type visitor interface {
	Before(node ast.Node) (w visitor)
	After(node ast.Node)
}
