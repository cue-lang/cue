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

// Package cue (v2) is the CUE value API: inspecting, combining, validating,
// and decoding CUE values.
//
// Unlike its predecessor, this package contains no value factory: values are
// created by a loader (see cuelang.org/go/cueload), which owns the runtime
// they belong to. Values created by different loaders may not be mixed.
//
// Values are lazy: methods that return Values construct descriptions and
// never evaluate, while methods whose answers leave the value domain (an
// error, a Go value, a field enumeration) force evaluation and take a
// [context.Context], which carries cancellation and an optional stats
// recorder (see cuelang.org/go/cue/stats). Errors are values in CUE, so
// failure flows through the lazy algebra like any other value and
// becomes a Go error only at a forcing point.
//
// Compared to v1, the following are intentionally absent: Context, Runtime,
// Instance, InstanceOrValue, BuildInstance/BuildFile/BuildExpr,
// Value.BuildInstance, Merge, Fill, and the Iterator types (superseded by
// iter.Seq2). Package-level provenance is provided by the loader
// (cueload.Loader.PackageOf, OriginOf).
package cue
