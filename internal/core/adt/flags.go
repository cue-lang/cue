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

package adt

// Flags describe the mode of evaluation for a vertex.
type Flags struct {
	// status is a remnant from evalv2, where it used to request a certain
	// state. It is still used here and there. TODO: remove
	status vertexStatus

	// condition and runmode indicates properties to satisfy for evalv4
	condition condition
	mode      runMode

	// concrete indicates whether the result should be concrete.
	concrete bool

	// checkTypos indicates whether to check for typos (closedness).
	checkTypos bool
}

var (
	FinalizeWithoutTypoCheck = Flags{
		status:     finalized,
		condition:  allKnown,
		mode:       finalize,
		checkTypos: false,
	}
)
