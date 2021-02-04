// Copyright 2021 The CUE Authors
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

// Package testing is a helper package for test packages in the CUE project.
// As such it should only be imported in _test.go files.
package cuetest

import (
	"fmt"
	"os"
)

// UpdateGoldenFiles determines whether testscript scripts should update txtar
// archives in the event of cmp failures. It corresponds to
// testscript.Params.UpdateGoldenFiles. See the docs for
// testscript.Params.UpdateGoldenFiles for more details.
var UpdateGoldenFiles = os.Getenv("CUE_UPDATE") != ""

// Condition adds support for CUE-specific testscript conditions within
// testscript scripts. The canonical case being [long] which evalutates to true
// when the long build tag is specified, as is used to indicate that long tests
// should be run.
func Condition(cond string) (bool, error) {
	switch cond {
	case "long":
		return Long, nil
	}
	return false, fmt.Errorf("unknown condition %v", cond)
}
