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
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/encoding/json"
	"cuelang.org/go/encoding/protobuf"
	"cuelang.org/go/internal/filetypes"
	"cuelang.org/go/internal/third_party/yaml"
)

type Decoder struct {
	cfg      *Config
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

	Force     bool // overwrite existing files.
	Stream    bool // will potentially write more than one document per file
	AllErrors bool

	EscapeHTML bool
	ProtoPath  []string
	Format     []format.Option
}

// NewDecoder returns a stream of non-rooted data expressions. The encoding
// type of f must be a data type, but does not have to be an encoding that
// can stream. stdin is used in case the file is "-".
func NewDecoder(f *build.File, cfg *Config) *Decoder {
	if cfg == nil {
		cfg = &Config{}
	}
	i := &Decoder{filename: f.Filename, cfg: cfg}
	i.next = func() (ast.Expr, error) {
		if i.err != nil {
			return nil, i.err
		}
		return nil, io.EOF
	}

	if file, ok := f.Source.(*ast.File); ok {
		i.file = file
		i.closer = ioutil.NopCloser(strings.NewReader(""))
		i.validate(file, f)
		return i
	}

	r, err := reader(f, cfg.Stdin)
	i.closer = r
	i.err = err
	if err != nil {
		return i
	}

	path := f.Filename
	switch f.Encoding {
	case build.CUE:
		i.file, i.err = parser.ParseFile(path, r, parser.ParseComments)
		i.validate(i.file, f)
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

func shouldValidate(i *filetypes.FileInfo) bool {
	// TODO: We ignore attributes for now. They should be enabled by default.
	return false ||
		!i.Definitions ||
		!i.Data ||
		!i.Optional ||
		!i.Constraints ||
		!i.References ||
		!i.Cycles ||
		!i.KeepDefaults ||
		!i.Incomplete ||
		!i.Imports ||
		!i.Docs
}

type validator struct {
	allErrors bool
	count     int
	errs      errors.Error
	fileinfo  *filetypes.FileInfo
}

func (d *Decoder) validate(f *ast.File, b *build.File) {
	if d.err != nil {
		return
	}
	fi, err := filetypes.FromFile(b, filetypes.Input)
	if err != nil {
		d.err = err
		return
	}
	if !shouldValidate(fi) {
		return
	}

	v := validator{fileinfo: fi, allErrors: d.cfg.AllErrors}
	ast.Walk(f, v.validate, nil)
	d.err = v.errs
}

func (v *validator) validate(n ast.Node) bool {
	if v.count > 10 {
		return false
	}

	i := v.fileinfo

	// TODO: Cycles

	ok := true
	check := func(n ast.Node, option bool, s string, cond bool) {
		if !option && cond {
			v.errs = errors.Append(v.errs, errors.Newf(n.Pos(),
				"%s not allowed in %s mode", s, v.fileinfo.Form))
			v.count++
			ok = false
		}
	}

	// For now we don't make any distinction between these modes.

	constraints := i.Constraints && i.Incomplete && i.Optional && i.References

	check(n, i.Docs, "comments", len(ast.Comments(n)) > 0)

	switch x := n.(type) {
	case *ast.CommentGroup:
		check(n, i.Docs, "comments", len(ast.Comments(n)) > 0)
		return false

	case *ast.ImportDecl, *ast.ImportSpec:
		check(n, i.Imports, "imports", true)

	case *ast.Field:
		check(n, i.Definitions, "definitions", x.Token == token.ISA)
		check(n, i.Data, "regular fields", x.Token != token.ISA)
		check(n, constraints, "optional fields", x.Optional != token.NoPos)

		_, _, err := ast.LabelName(x.Label)
		check(n, constraints, "optional fields", err != nil)

		check(n, i.Attributes, "attributes", len(x.Attrs) > 0)
		ast.Walk(x.Value, v.validate, nil)
		return false

	case *ast.UnaryExpr:
		switch x.Op {
		case token.MUL:
			check(n, i.KeepDefaults, "default values", true)
		case token.SUB, token.ADD:
			// The parser represents negative numbers as an unary expression.
			// Allow one `-` or `+`.
			_, ok := x.X.(*ast.BasicLit)
			check(n, constraints, "expressions", !ok)
		case token.LSS, token.LEQ, token.EQL, token.GEQ, token.GTR,
			token.NEQ, token.NMAT, token.MAT:
			check(n, constraints, "constraints", true)
		default:
			check(n, constraints, "expressions", true)
		}

	case *ast.BinaryExpr, *ast.ParenExpr, *ast.IndexExpr, *ast.SliceExpr,
		*ast.CallExpr, *ast.Comprehension, *ast.ListComprehension,
		*ast.Interpolation:
		check(n, constraints, "expressions", true)

	case *ast.Ellipsis:
		check(n, constraints, "ellipsis", true)

	case *ast.Ident, *ast.SelectorExpr, *ast.Alias:
		check(n, i.References, "references", true)

	default:
		// Other types are either always okay or handled elsewhere.
	}
	return ok
}
