// Copyright 2025 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package format provides functionality for pretty-printing CUE values.
// These types need to be in a separate package to avoid import cycles.
package format

// Printer is the interface used to print CUE values. The only implementation so
// far is the one in internal/core/debug. Note that most packages cannot
// directly import the debug package.
type Printer interface {
	// ReplaceArg is a function that may be called to replace arguments to
	// errors. This is mostly used for cycle detection.
	ReplaceArg(x any) (r any, wasReplaced bool)
}
