// Copyright 2021 CUE Authors
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

package types

import (
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"
)

// ToInternal converts the value x to internal form from
// a cue.Value. If it's not a cue.Value, it returns nil, nil.
// This is initialized by the top level cue package to avoid
// a cyclic dependency from the evaluation logic.
var ToInternal func(x interface{}) (*runtime.Runtime, *adt.Vertex)
