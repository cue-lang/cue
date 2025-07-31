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

// This file contains types to help in the transition from the old to new
// evaluation model.

func unreachableForDev(c *OpContext) {
	if c.isDevVersion() {
		panic("unreachable for development version")
	}
}

type combinedFlags struct {
	status    vertexStatus
	condition condition
	mode      runMode
}

// oldOnly indicates that a Vertex should only be evaluated for the old
// evaluator.
func oldOnly(state vertexStatus) combinedFlags {
	return combinedFlags{
		status:    state,
		condition: allKnown,
		mode:      ignore,
	}
}

func combineMode(cond condition, mode runMode) combinedFlags {
	return combinedFlags{
		status:    0,
		condition: cond,
		mode:      mode,
	}
}

func attempt(state vertexStatus, cond condition) combinedFlags {
	return combinedFlags{
		status:    state,
		condition: cond,
		mode:      attemptOnly,
	}
}

func require(state vertexStatus, cond condition) combinedFlags {
	return combinedFlags{
		status:    state,
		condition: cond,
		mode:      yield,
	}
}

func final(state vertexStatus, cond condition) combinedFlags {
	return combinedFlags{
		status:    state,
		condition: cond,
		mode:      finalize,
	}
}

func deprecated(c *OpContext, state vertexStatus) combinedFlags {
	// if c.isDevVersion() {
	// 	panic("calling function may not be used in new evaluator")
	// }
	return combinedFlags{
		status:    state,
		condition: 0,
		mode:      0,
	}
}

func (f combinedFlags) vertexStatus() vertexStatus {
	return f.status
}

func (f combinedFlags) withVertexStatus(x vertexStatus) combinedFlags {
	return combinedFlags{
		status:    x,
		condition: f.condition,
		mode:      f.mode,
	}
}

func (f combinedFlags) conditions() condition {
	return f.condition
}

func (f combinedFlags) runMode() runMode {
	return f.mode
}

func (f combinedFlags) ignore() bool {
	return f.mode == ignore
}
