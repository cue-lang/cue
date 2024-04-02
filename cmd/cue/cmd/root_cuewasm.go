// Copyright 2024 The CUE Authors
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

//go:build cuewasm

package cmd

import (
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/interpreter/wasm"
)

// The wasm interpreter can be enabled by default once we are ready to ship the feature.
// For now, it's not ready, and makes cue binaries heavier by over 2MiB.
func init() {
	rootContextOptions = append(rootContextOptions, cuecontext.Interpreter(wasm.New()))
}
