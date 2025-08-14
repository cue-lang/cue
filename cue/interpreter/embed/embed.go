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
// This package is EXPERIMENTAL and subject to change.
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
// allowEmptyGlob
//
// By default, a glob pattern that matches no files results in an error. When
// allowEmptyGlob is present, a glob pattern with no matches will return an
// empty struct instead of an error. This option is only supported with glob patterns.
//
// # Limitations
//
// The embed interpreter currently does not support:
// - stream values, such as .ndjson or YAML streams.
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
//
//	// include all YAML files in the z directory, but allow empty result
//	// if no files match (returns empty struct instead of error)
//	optionalFiles: _ @embed(glob=z/*.yaml, allowEmptyGlob)
package embed

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/encoding"
	"cuelang.org/go/internal/filetypes"
	"cuelang.org/go/internal/value"
	pkgpath "cuelang.org/go/pkg/path"
)

// TODO: record files in build.Instance

// interpreter is a [cuecontext.ExternInterpreter] for embedded files.
type interpreter struct{}

// Note that [cuecontext.ExternInterpreter] is just an alias for [runtime.Interpreter]
// but because the [cuelang.org/go/cue/cuecontext] package depends on embedding
// we cannot refer to that type directly.

// New returns a new interpreter for embedded files as a
// [cuelang.org/go/cue/cuecontext.ExternInterpreter] suitable for
// passing to [cuelang.org/go/cue/cuecontext.New].
func New() runtime.Interpreter {
	return interpreter{}
}

func (i interpreter) Kind() string {
	return "embed"
}

// NewCompiler returns a compiler that can decode and embed files that exist
// within a CUE module.
func (i interpreter) NewCompiler(b *build.Instance, r *runtime.Runtime) (runtime.Compiler, errors.Error) {
	if b.Module == "" {
		return nil, errors.Newf(token.Pos{}, "cannot embed files when not in a module")
	}
	if b.Root == "" {
		return nil, errors.Newf(token.Pos{}, "cannot embed files: no module root found")
	}
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

	// file system cache
	dir string
	fs  fs.StatFS
	pos token.Pos
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

	allowEmptyGlob, err := a.Flag(0, "allowEmptyGlob")
	if err != nil {
		return nil, errors.Promote(err, "invalid allowEmptyGlob argument")
	}

	c.opCtx = adt.NewContext((*runtime.Runtime)(c.runtime), nil)

	pos := a.Pos
	c.pos = pos

	// Jump through some hoops to get file operations to behave the same for
	// Windows and Unix.
	// TODO: obtain a fs.FS from load or something similar.
	dir := filepath.Dir(pos.File().Name())
	if c.dir != dir {
		c.fs = os.DirFS(dir).(fs.StatFS) // Documented as implementing fs.StatFS
		c.dir = dir
	}

	switch {
	case file == "" && glob == "":
		return nil, errors.Newf(a.Pos, "attribute must have file or glob field")

	case file != "" && glob != "":
		return nil, errors.Newf(a.Pos, "attribute cannot have both file and glob field")
	case allowEmptyGlob && glob == "":
		return nil, errors.Newf(a.Pos, "allowEmptyGlob must be specified with a glob field")
	case file != "":
		return c.processFile(file, typ, scope)

	default: // glob != "":
		return c.processGlob(glob, typ, allowEmptyGlob, scope)
	}
}

func (c *compiler) processFile(file, scope string, schema adt.Value) (adt.Expr, errors.Error) {
	file, err := c.clean(file)
	if err != nil {
		return nil, err
	}
	for dir := path.Dir(file); dir != "."; dir = path.Dir(dir) {
		if _, err := c.fs.Stat(path.Join(dir, "cue.mod")); err == nil {
			return nil, errors.Newf(c.pos, "cannot embed file %q: in different module", file)
		}
	}

	return c.decodeFile(file, scope, schema)
}

func (c *compiler) processGlob(glob, scope string, allowEmptyGlob bool, schema adt.Value) (adt.Expr, errors.Error) {
	glob, ce := c.clean(glob)
	if ce != nil {
		return nil, ce
	}

	// Validate that the glob pattern is valid per [pkgpath.Match].
	// Note that we use Unix match semantics because all embed paths are Unix-like.
	if _, err := pkgpath.Match(glob, "", pkgpath.Unix); err != nil {
		return nil, errors.Wrapf(err, c.pos, "invalid glob pattern %q", glob)
	}

	// If we do not have a type, ensure the extension of the base is fully
	// specified, i.e. does not contain any meta characters as specified by
	// path.Match.
	if scope == "" {
		ext := path.Ext(path.Base(glob))
		if ext == "" || strings.ContainsAny(ext, "*?[\\") {
			return nil, errors.Newf(c.pos, "extension not fully specified; type argument required")
		}
	}

	m := &adt.StructLit{}

	matches, err := fsGlob(c.fs, glob)
	if err != nil {
		return nil, errors.Promote(err, "failed to match glob")
	}
	if len(matches) == 0 && !allowEmptyGlob {
		return nil, errors.Newf(c.pos, "no matches for glob pattern %q", glob)
	}

	dirs := make(map[string]string)
	for _, f := range matches {
		// TODO: lots of stat calls happening in this MVP so another won't hurt.
		// We don't support '**' initially, and '*' only matches files, so skip
		// any directories.
		if fi, err := c.fs.Stat(f); err != nil {
			return nil, errors.Newf(c.pos, "failed to stat %s: %v", f, err)
		} else if fi.IsDir() {
			continue
		}
		// Add all parents of the embedded file that
		// aren't the current directory (if there's a cue.mod
		// in the current directory, that's the current module
		// not nested).
		for dir := path.Dir(f); dir != "."; dir = path.Dir(dir) {
			dirs[dir] = f
		}

		expr, err := c.decodeFile(f, scope, schema)
		if err != nil {
			return nil, err
		}

		m.Decls = append(m.Decls, &adt.Field{
			Label: c.opCtx.StringLabel(f),
			Value: expr,
		})
	}
	// Check that none of the matches were in a nested module
	// directory.
	for dir, f := range dirs {
		if _, err := c.fs.Stat(path.Join(dir, "cue.mod")); err == nil {
			return nil, errors.Newf(c.pos, "cannot embed file %q: in different module", f)
		}
	}
	return m, nil
}

func (c *compiler) clean(s string) (string, errors.Error) {
	file := path.Clean(s)
	if file != s {
		return file, errors.Newf(c.pos, "path not normalized, use %q instead", file)
	}
	if path.IsAbs(file) {
		return "", errors.Newf(c.pos, "only relative files are allowed")
	}
	if file == ".." || strings.HasPrefix(file, "../") {
		return "", errors.Newf(c.pos, "cannot refer to parent directory")
	}
	return file, nil
}

// fsGlob is like [fs.Glob] but only includes dot-prefixed files
// when the dot is explictly present in an element.
// TODO: add option for including dot files?
func fsGlob(fsys fs.FS, pattern string) ([]string, error) {
	pattern = path.Clean(pattern)
	matches, err := fs.Glob(fsys, pattern)
	if err != nil {
		return nil, err
	}
	patElems := strings.Split(pattern, "/")
	included := func(m string) bool {
		for i, elem := range strings.Split(m, "/") {
			// Technically there should never be more elements in m than
			// there are in patElems, but be defensive and check bounds just in case.
			if strings.HasPrefix(elem, ".") && (i >= len(patElems) || !strings.HasPrefix(patElems[i], ".")) {
				return false
			}
		}
		return true
	}

	i := 0
	for _, m := range matches {
		if included(m) {
			matches[i] = m
			i++
		}
	}
	return matches[:i], nil
}

func (c *compiler) decodeFile(file, scope string, schema adt.Value) (adt.Expr, errors.Error) {
	// Do not use the most obvious filetypes.Input in order to disable "auto"
	// mode.
	f, err := filetypes.ParseFileAndType(file, scope, filetypes.Def)
	if err != nil {
		return nil, errors.Promote(err, "invalid file type")
	}

	// Open and pre-load the file system using fs.FS, instead of relying
	r, err := c.fs.Open(file)
	if err != nil {
		return nil, errors.Newf(c.pos, "open %v: no such file or directory", file)
	}
	defer r.Close()

	info, err := r.Stat()
	if err != nil {
		return nil, errors.Promote(err, "failed to decode file")
	}
	if info.IsDir() {
		return nil, errors.Newf(c.pos, "cannot embed directories")
	}
	f.Source = r

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
		// TODO: support streaming values
		return nil, errors.Newf(c.pos, "streaming not implemented: found more than one value in file")
	}

	// TODO: each of these encodings should probably be supported in the future
	switch f.Encoding {
	case build.CUE:
		return nil, errors.Newf(c.pos, "encoding %q not (yet) supported", f.Encoding)
	case build.JSONL:
		return nil, errors.Newf(c.pos, "encoding %q not (yet) supported: requires support for streaming", f.Encoding)
	case build.BinaryProto, build.TextProto:
		return nil, errors.Newf(c.pos, "encoding %q not (yet) supported: requires support for schema-guided decoding", f.Encoding)
	}

	val := c.runtime.BuildFile(n)
	if err := val.Err(); err != nil {
		return nil, errors.Promote(err, "failed to build file")
	}

	_, v := value.ToInternal(val)
	return v, nil
}
