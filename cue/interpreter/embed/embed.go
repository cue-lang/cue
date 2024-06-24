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

// Package embed provides capabilities to CUE to embed any file that resides
// within a CUE module into CUE either verbatim or decoded.
//
// # Overview
//
// To enable file embedding, a file must include the file-level @extern(embed)
// attribute. This allows a quick glance to see if a file embeds any files at
// all. This allows the @embed attribute to be used to load a file within a CUE
// module into a field.
//
// References to files are always relative to directory in which the referring
// file resides. Only files that exist within the CUE module are accessible.
//
// # The @embed attribute
//
// There are two main ways to embed files which are distinguished by the file
// and glob arguments. The @embed attribute supports the following arguments:
//
// file=$filename
//
// The use of the file argument tells embed to load a single file into the
// field. This argument many not be used in conjunction with the glob argument.
//
// glob=$pattern
//
// The use of the glob argument tells embed to load multiple files into the
// field as a map of file paths to the decoded values. The paths are normalized
// to use forward slashes. This argument may not be used in conjunction with the
// file argument.
//
// type=$type
//
// By default, the file type is interpreted based on the file extension. This
// behavior can be overridden by the type argument. See cue help filetypes for
// the list of supported types. This field is required if a file extension is
// unknown, or if a wildcard is used for the file extension in the glob pattern.
//
// # Limitations
//
// The embed interpreter currently does not support:
// - stream values, such as .ldjson or YAML streams.
// - schema-based decoding, such as needed for textproto
//
// # Example
//
//	@extern(embed)
//
//	package foo
//
//	// interpreted as JSON
//	a: _ @embed(file="file1.json") // the quotes are optional here
//
//	// interpreted the same file as JSON schema
//	#A: _ @embed(file=file1.json, type=jsonschema)
//
//	// interpret a proprietary extension as OpenAPI represented as YAML
//	b: _ @embed(file="file2.crd", type=openapi+yaml)
//
//	// include all YAML files in the x directory interpreted as YAML
//	// The result is a map of file paths to the decoded YAML values.
//	files: _ @embed(glob=x/*.yaml)
//
//	// include all files in the y directory as a map of file paths to binary
//	// data. The entries are unified into the same map as above.
//	files: _ @embed(glob=y/*.*, type=binary)
package embed

import (
	"path"
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

// NewCompiler returns a compiler that can decode and embed files that exist
// within a CUE module.
func (i *interpreter) NewCompiler(b *build.Instance, r *runtime.Runtime) (runtime.Compiler, errors.Error) {
	return &compiler{
		b:       b,
		runtime: (*cue.Context)(r),
	}, nil
}

// A compiler is a [runtime.Compiler] that allows embedding files into CUE
// values.
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
	file, err := c.clean(file)
	if err != nil {
		return nil, err
	}

	return c.decodeFile(file, scope, schema)
}

func (c *compiler) processGlob(file, scope string, schema adt.Value) (adt.Expr, errors.Error) {
	file, ce := c.clean(file)
	if ce != nil {
		return nil, ce
	}

	if strings.Contains(file, "**") {
		return nil, errors.Newf(token.NoPos, "double star not supported in glob")
	}

	// TODO: Glob is probably incorrect, as it behaves differently on Windows.
	// We also want to unsure that globbing starts relative to the current file,
	// instead of the current directory, and that it only matches files within
	// the CUE module.
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

func (c *compiler) clean(s string) (string, errors.Error) {
	file := path.Clean(s)
	if file != s {
		return file, errors.Newf(token.NoPos, "path not normalized, use %q instead", file)
	}
	if filepath.IsAbs(file) {
		return "", errors.Newf(token.NoPos, "only relative files are allowed")
	}
	if strings.HasPrefix(file, "..") {
		return "", errors.Newf(token.NoPos, "cannot refer to parent directory")
	}
	return file, nil
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
		return nil, errors.Newf(token.NoPos, "streaming not implemented: found more than one value in file")
	}

	switch f.Encoding {
	case build.JSONL:
		return nil, errors.Newf(token.NoPos, "encoding %q not (yet) supported: requires support for streaming", f.Encoding)
	case build.BinaryProto, build.TextProto:
		return nil, errors.Newf(token.NoPos, "encoding %q not (yet) supported: requires support for schema-guided decoding", f.Encoding)
	}

	val := c.runtime.BuildFile(n)
	if err := val.Err(); err != nil {
		return nil, errors.Promote(err, "failed to build file")
	}

	_, v := value.ToInternal(val)
	return v, nil
}
