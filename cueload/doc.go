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

// Package cueload loads CUE packages and data files and converts them to
// CUE values. It replaces cuelang.org/go/cue/load.
//
// The central types are [Loader] — the configured, concurrency-safe,
// caching environment (filesystem, module registry, codecs, evaluator) —
// and [Source] — an immutable description of how to produce a stream of
// CUE values. Sources are built from leaves such as [Pkg] and [Decode]
// and shaped by combinators such as [Unify], [Validate], and [At]; a
// Loader interprets them via [Loader.Load]. A structural tier ([Package],
// [Module], [Doc]) exposes the same operations at the syntax level for
// tools that need more than values.
//
// A zero Config is hermetic: it reads no environment variables, performs
// no network access, and consults no clock; the host filesystem is the
// only default dependency. Command-line-shaped behavior (file qualifiers
// like "json:", per-command defaults, tag variables such as now and cwd)
// lives in cuelang.org/go/cueload/cli.
package cueload
