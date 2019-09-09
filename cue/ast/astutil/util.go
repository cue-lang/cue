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

package astutil

import "cuelang.org/go/cue/ast"

// CopyComments associates comments of one node with another.
// It may change the relative position of comments.
func CopyComments(to, from ast.Node) {
	if from == nil {
		return
	}
	ast.SetComments(to, from.Comments())
}

// CopyPosition sets the position of one node to another.
func CopyPosition(to, from ast.Node) {
	if from == nil {
		return
	}
	ast.SetPos(to, from.Pos())
}

// CopyMeta copies comments and position information from one node to another.
// It returns the destination node.
func CopyMeta(to, from ast.Node) ast.Node {
	if from == nil {
		return to
	}
	ast.SetComments(to, from.Comments())
	ast.SetPos(to, from.Pos())
	return to
}
