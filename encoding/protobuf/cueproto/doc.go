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

// Package cueproto contains the Go code generated from cue.proto, which
// defines the CUE-specific options for use in proto definitions, such as
// (cue.val). Go code generated from proto definitions which use these
// options imports this package, so that it compiles and registers the
// extensions.
package cueproto

// The cue.pb.go files here and in the sibling cue package are generated
// without needing protoc; see [cuelang.org/go/internal/cmd/genproto].

//go:generate go run cuelang.org/go/internal/cmd/genproto
