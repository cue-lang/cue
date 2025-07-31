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
