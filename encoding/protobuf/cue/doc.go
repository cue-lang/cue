// Copyright 2026 CUE Authors
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

// Package cue forwards to [cuelang.org/go/encoding/protobuf/cueproto].
// It holds the Go code generated from the cue/cue.proto shim, which exists
// for compatibility with proto definitions importing "cue/cue.proto", the
// path at which the CUE options were originally published.
//
// Deprecated: use [cuelang.org/go/encoding/protobuf/cueproto].
// This package and the cue/cue.proto shim will be removed in v0.19.
package cue

// TODO(v0.19): remove this package along with the cue/cue.proto shim.
