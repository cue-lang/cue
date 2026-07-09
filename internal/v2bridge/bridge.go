// Copyright 2026 The CUE Authors
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

// Package v2bridge connects cuelang.org/go/cue/v2 to internal packages
// that need to construct cue/v2 values, such as a loader. It exists so
// that such packages do not need cue/v2 to export internal constructors
// in its public API.
package v2bridge

import (
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"
)

// NewVertexValue is set by cuelang.org/go/cue/v2 upon initialization.
// It returns a cue/v2 Value (typed any here to avoid referring to the
// cue/v2 package) for the given vertex, owned by the given runtime.
// Callers assert the result to the cue/v2 Value type.
var NewVertexValue func(rt *runtime.Runtime, v *adt.Vertex) any
