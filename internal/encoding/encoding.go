// Copyright 2020 CUE Authors
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

package encoding

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strings"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/encoding/json"
	"cuelang.org/go/encoding/protobuf"
	"cuelang.org/go/internal/filetypes"
	"cuelang.org/go/internal/third_party/yaml"
)

type Decoder struct {
	closer   io.Closer
	next     func() (ast.Expr, error)
	expr     ast.Expr
	file     *ast.File
	filename string // may change on iteration for some formats
	index    int
	err      error
}

func (i *Decoder) Expr() ast.Expr   { return i.expr }
func (i *Decoder) Filename() string { return i.filename }
func (i *Decoder) Index() int       { return i.index }
func (i *Decoder) Done() bool       { return i.err != nil }

func (i *Decoder) Next() {
	if i.err == nil {
		i.expr, i.err = i.next()
		i.index++
	}
}

func toFile(x ast.Expr) *ast.File {
	switch x := x.(type) {
	case nil:
		return nil
	case *ast.StructLit:
		return &ast.File{Decls: x.Elts}
	default:
		return &ast.File{Decls: []ast.Decl{&ast.EmbedDecl{Expr: x}}}
	}
}

func valueToFile(v cue.Value) *ast.File {
	switch x := v.Syntax().(type) {
	case *ast.File:
		return x
	case ast.Expr:
		return toFile(x)
	default:
		panic("unrreachable")
	}
}

func (i *Decoder) File() *ast.File {
	if i.file != nil {
		return i.file
	}
	return toFile(i.expr)
}

func (i *Decoder) Err() error {
	if i.err == io.EOF {
		return nil
	}
	return i.err
}

func (i *Decoder) Close() {
	i.closer.Close()
}

type Config struct {
	Mode filetypes.Mode

	// Out specifies an overwrite destination.
	Out    io.Writer
	Stdin  io.Reader
	Stdout io.Writer

	Force bool // overwrite existing files.

	EscapeHTML bool
	ProtoPath  []string
}

// NewDecoder returns a stream of non-rooted data expressions. The encoding
// type of f must be a data type, but does not have to be an encoding that
// can stream. stdin is used in case the file is "-".
func NewDecoder(f *build.File, cfg *Config) *Decoder {
	r, err := reader(f, cfg.Stdin)
	i := &Decoder{
		closer:   r,
		err:      err,
		filename: f.Filename,
		next: func() (ast.Expr, error) {
			if err == nil {
				err = io.EOF
			}
			return nil, io.EOF
		},
	}
	if err != nil {
		return i
	}

	path := f.Filename
	switch f.Encoding {
	case build.JSON, build.JSONL:
		i.next = json.NewDecoder(nil, path, r).Extract
		i.Next()
	case build.YAML:
		d, err := yaml.NewDecoder(path, r)
		i.err = err
		i.next = d.Decode
		i.Next()
	case build.Text:
		b, err := ioutil.ReadAll(r)
		i.err = err
		i.expr = ast.NewString(string(b))
	case build.Protobuf:
		paths := &protobuf.Config{Paths: cfg.ProtoPath}
		i.file, i.err = protobuf.Extract(path, r, paths)
	default:
		i.err = fmt.Errorf("unsupported stream type %q", f.Encoding)
	}

	return i
}

func reader(f *build.File, stdin io.Reader) (io.ReadCloser, error) {
	switch s := f.Source.(type) {
	case nil:
		// Use the file name.
	case string:
		return ioutil.NopCloser(strings.NewReader(s)), nil
	case []byte:
		return ioutil.NopCloser(bytes.NewReader(s)), nil
	case *bytes.Buffer:
		// is io.Reader, but it needs to be readable repeatedly
		if s != nil {
			return ioutil.NopCloser(bytes.NewReader(s.Bytes())), nil
		}
	default:
		return nil, fmt.Errorf("invalid source type %T", f.Source)
	}
	// TODO: should we allow this?
	if f.Filename == "-" {
		return ioutil.NopCloser(stdin), nil
	}
	return os.Open(f.Filename)
}
