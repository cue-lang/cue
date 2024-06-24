// Copyright 2024 CUE Authors
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

package embed

import (
	"path/filepath"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/encoding"
	"cuelang.org/go/internal/filetypes"
	"cuelang.org/go/internal/value"
)

// TODO:
// - record files in build.Instance
// - support stream values
// - support schema-based decoding

// interpreter is a [cuecontext.ExternInterpreter] for embedded files.
type interpreter struct{}

// New returns a new interpreter for embedded files as a
// [cuecontext.ExternInterpreter] suitable for passing to [cuecontext.New].
func New() cuecontext.ExternInterpreter {
	return &interpreter{}
}

func (i *interpreter) Kind() string {
	return "embed"
}

// NewCompiler returns a Wasm compiler that services the specified
// build.Instance.
func (i *interpreter) NewCompiler(b *build.Instance, r *runtime.Runtime) (runtime.Compiler, errors.Error) {
	return &compiler{
		b:       b,
		runtime: (*cue.Context)(r),
	}, nil
}

// A compiler is a [runtime.Compiler]
// that provides Wasm functionality to the runtime.
type compiler struct {
	b       *build.Instance
	runtime *cue.Context
	opCtx   *adt.OpContext
}

// Compile interprets an embed attribute to either load a file
// (@embed(file=...)) or a glob of files (@embed(glob=...)).
// and decodes the given files.
func (c *compiler) Compile(funcName string, scope adt.Value, a *internal.Attr) (adt.Expr, errors.Error) {
	file, _, err := a.Lookup(0, "file")
	if err != nil {
		return nil, errors.Promote(err, "invalid attribute")
	}

	glob, _, err := a.Lookup(0, "glob")
	if err != nil {
		return nil, errors.Promote(err, "invalid attribute")
	}

	typ, _, err := a.Lookup(0, "type")
	if err != nil {
		return nil, errors.Promote(err, "invalid type argument")
	}

	c.opCtx = adt.NewContext((*runtime.Runtime)(c.runtime), nil)

	switch {
	case file == "" && glob == "":
		return nil, errors.Newf(token.NoPos, "attribute must have file or glob field")

	case file != "" && glob != "":
		return nil, errors.Newf(token.NoPos, "attribute cannot have both file and glob field")

	case file != "":
		return c.processFile(file, typ, scope)

	default: // glob != "":
		return c.processGlob(glob, typ, scope)
	}
}

func (c *compiler) processFile(file, scope string, schema adt.Value) (adt.Expr, errors.Error) {
	return c.decodeFile(file, scope, schema)
}

func (c *compiler) processGlob(file, scope string, schema adt.Value) (adt.Expr, errors.Error) {
	if strings.Contains(file, "**") {
		return nil, errors.Newf(token.NoPos, "double star not supported in glob")
	}

	matches, err := filepath.Glob(file)
	if err != nil {
		return nil, errors.Promote(err, "failed to match glob")
	}

	m := &adt.StructLit{}

	for _, f := range matches {
		expr, err := c.decodeFile(f, scope, schema)
		if err != nil {
			return nil, err
		}

		m.Decls = append(m.Decls, &adt.Field{
			Label: c.opCtx.StringLabel(filepath.ToSlash(f)),
			Value: expr,
		})
	}

	return m, nil
}

func (c *compiler) decodeFile(file, scope string, schema adt.Value) (adt.Expr, errors.Error) {
	// Do not use the most obvious filetypes.Input in order to disable "auto"
	// mode.
	f, err := filetypes.ParseFileAndType(file, scope, filetypes.Def)
	if err != nil {
		return nil, errors.Promote(err, "invalid file type")
	}

	// TODO: this really should be done at the start of the build process.
	// c.b.ExternFiles = append(c.b.ExternFiles, f)

	config := &encoding.Config{
		// TODO: schema is currently the wrong schema, which is a bug in
		// internal/core/runtime. There is also an outstanding design choice:
		// do we imply the schema from the schema of the current field, or do
		// we explicitly enable schema-based encoding with a "schema" argument.
		// In the case of YAML it seems to be better to be explicit. In the case
		// of textproto it seems to be more convenient to do it implicitly.
		// Schema: value.Make(c.opCtx, schema),
	}

	d := encoding.NewDecoder(c.runtime, f, config)
	if err := d.Err(); err != nil {
		return nil, errors.Promote(err, "failed to decode file")
	}

	defer d.Close()

	n := d.File()

	if d.Next(); !d.Done() {
		return nil, errors.Newf(token.NoPos, "stream values not implemented")
	}

	val := c.runtime.BuildFile(n)
	if err := val.Err(); err != nil {
		return nil, errors.Promote(err, "failed to build file")
	}

	_, v := value.ToInternal(val)
	return v, nil
}
