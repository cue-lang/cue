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

package cueload

import (
	"context"
	"io/fs"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/stats"
	"cuelang.org/go/cuecodec"
	"cuelang.org/go/mod/modconfig"
)

// Config configures a [Loader]. The zero value is valid and hermetic:
// it reads no environment variables, performs no network access, and
// consults no clock. The single exception is that when FS is nil the
// host filesystem is used and Dir defaults to the process working
// directory.
//
// A Config must not be modified after being passed to [New].
type Config struct {
	// Dir is the base directory for relative paths and module discovery.
	// It defaults to the process working directory, or to "/" when FS is
	// set.
	Dir string

	// FS, if non-nil, is the filesystem all files are loaded from, in
	// place of the host OS. Paths within FS are slash-separated;
	// "/"-rooted paths are interpreted relative to the root of FS.
	FS fs.FS

	// ModuleRoot optionally pins the module root directory. If empty,
	// the module root is discovered by walking up from Dir.
	ModuleRoot string

	// Registry resolves modules for imports outside the main module.
	// If nil, such imports are errors. Use modconfig.NewRegistry to
	// build one from the environment.
	Registry modconfig.Registry

	// Codecs is the set of file formats understood by the loader.
	// If nil, cuecodec.Default is used (cue, json, yaml).
	Codecs *cuecodec.Set

	// Evaluator configures the evaluation runtime owned by the loader.
	// If nil, defaults are used.
	Evaluator *EvaluatorConfig

	// Tags provides values for @tag() attributes, and BuildTags the set
	// of names considered set by @if() file attributes. TagVars defines
	// named variables usable as @tag(name,var=x); the standard
	// side-effecting set (now, cwd, os, ...) is provided by cueload/cli,
	// not here.
	//
	// A Tags entry whose value is non-empty provides the value for the
	// @tag() attributes with the entry's key as their name; the value is
	// interpreted according to the attribute's type option. An entry
	// with an empty value selects the shorthand of that name declared by
	// a @tag(...,short=...) attribute.
	Tags      map[string]string
	BuildTags []string
	TagVars   map[string]TagVar

	// IncludeTests and IncludeTools include _test.cue and _tool.cue
	// files when loading packages.
	IncludeTests bool
	IncludeTools bool

	// ParseFile, if non-nil, replaces the parsing of CUE files,
	// allowing tools to intercept or cache parses.
	ParseFile func(name string, src []byte, cfg parser.Config) (*ast.File, error)

	// Resolve, if non-nil, is consulted for import paths before module
	// and standard-library resolution, allowing virtual and
	// host-implemented packages.
	Resolve PackageResolver
}

// EvaluatorConfig configures the evaluation runtime owned by a Loader.
type EvaluatorConfig struct {
	// Version selects the evaluator version. The zero value selects
	// the current default.
	Version EvalVersion

	// Debug holds CUE_DEBUG-style debug options.
	Debug string

	// Injections configures compile-time injection mechanisms such as
	// @embed and wasm support.
	//
	// TODO(cueload): injections are not yet supported by this loader;
	// setting this field is an error. The injection mechanism in
	// internal/core/runtime is tied to build.Instance, which this
	// loader does not use. In particular, @embed is NOT enabled by
	// default yet, unlike cuecontext.New.
	Injections []Injection

	// Recorder, if non-nil, receives evaluation statistics for all
	// operations the loader itself runs (loading, building, and
	// validating values), unless overridden per-operation via
	// stats.WithRecorder.
	//
	// TODO(cueload): operations invoked directly on values created by
	// the loader currently report to the runtime's internal recorder
	// instead; directing those here needs a runtime-level hook.
	Recorder *stats.Recorder
}

// EvalVersion selects an evaluator version.
type EvalVersion int

const (
	EvalDefault EvalVersion = iota
	EvalV2
	EvalV3
)

// Injection is a compile-time injection mechanism (@embed, wasm, ...).
// Implementations are provided by cue/inject/... packages.
type Injection interface {
	Kind() string
}

// TagVar defines a lazily-evaluated variable for @tag(name,var=x)
// injection.
type TagVar struct {
	// Func returns the value of the variable. It is invoked at most
	// once per loader.
	Func func() (ast.Expr, error)

	// Description documents the variable in help output.
	Description string
}

// PackageResolver resolves import paths to packages, before module and
// standard-library resolution. Returning (nil, nil) declines, letting
// standard resolution proceed.
//
// Implementations typically construct their result with
// [Loader.NewPackage] or [Loader.NewValuePackage].
type PackageResolver interface {
	ResolvePackage(ctx context.Context, path ast.ImportPath) (*Package, error)
}

var _ = func(p ast.ImportPath) {}
