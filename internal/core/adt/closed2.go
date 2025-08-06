// Copyright 2020 CUE Authors
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

package adt

// CloseDef defines how individual fieldSets (corresponding to conjuncts)
// combine to determine whether a field is contained in a closed set.
//
// A CloseDef combines multiple conjuncts and embeddings. All CloseDefs are
// stored in slice. References to other CloseDefs are indices within this slice.
// Together they define the top of the tree of the expression tree of how
// conjuncts combine together (a canopy).

// isComplexStruct reports whether the Closed information should be copied as a
// subtree into the parent node using InsertSubtree. If not, the conjuncts can
// just be inserted at the current ID.
func isComplexStruct(ctx *OpContext, v *Vertex) bool {
	return v.IsClosedStruct()
}
