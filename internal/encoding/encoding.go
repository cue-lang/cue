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

// TODO: make this package public in cuelang.org/go/encoding
// once stabilized.

package encoding

import (
	"fmt"
	"io"
	"maps"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/literal"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/encoding/ini"
	"cuelang.org/go/encoding/json"
	"cuelang.org/go/encoding/jsonschema"
	"cuelang.org/go/encoding/openapi"
	"cuelang.org/go/encoding/protobuf"
	"cuelang.org/go/encoding/protobuf/jsonpb"
	"cuelang.org/go/encoding/protobuf/textproto"
	"cuelang.org/go/encoding/toml"
	"cuelang.org/go/encoding/xml/koala"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/cueexperiment"
	"cuelang.org/go/internal/encoding/yaml"
	"cuelang.org/go/internal/filetypes"
	"cuelang.org/go/internal/source"
	"cuelang.org/go/internal/valuecodec"
	"golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
)

type Decoder struct {
	ctx            *cue.Context
	cfg            *Config
	closer         io.Closer
	next           func() (ast.Expr, error)
	nextValue      func() (cue.Value, error)
	rewriteFunc    rewriteFunc
	interpretFunc  interpretFunc
	autoInterpret  bool
	interpretation build.Interpretation
	expr           ast.Expr
	file           *ast.File
	value          cue.Value
	hasValue       bool
	valueProjected bool
	filename       string // may change on iteration for some formats
	index          int
	size           int // length of the source file if known; -1 otherwise
	err            error
}

type interpretFunc func(cue.Value) (file *ast.File, err error)
type rewriteFunc func(*ast.File) (file *ast.File, err error)

func (i *Decoder) Filename() string { return i.filename }

// Interpretation returns the current interpretation detected by Detect.
func (i *Decoder) Interpretation() build.Interpretation {
	return i.interpretation
}
func (i *Decoder) Index() int { return i.index }
func (i *Decoder) Done() bool { return i.err != nil }

func (i *Decoder) Next() {
	if i.err != nil {
		return
	}
	// Decoder level
	i.file = nil
	i.expr = nil
	i.value = cue.Value{}
	i.hasValue = false
	i.valueProjected = false
	if i.nextValue != nil {
		i.value, i.err = i.nextValue()
		i.index++
		if i.err != nil {
			return
		}
		if !i.value.Exists() {
			// A non-existent value is a codec contract violation, not a
			// semantic bottom: error values exist and remain valid payload
			// (formats such as CUE wire intentionally carry them), whereas
			// a zero cue.Value has no document behind it and would flow
			// through as silently empty or, under a schema, be replaced by
			// the schema entirely.
			i.err = fmt.Errorf("value-plane decoder for %q returned a non-existent value", i.filename)
			return
		}
		i.hasValue = true
		i.doInterpret()
		return
	}
	i.expr, i.err = i.next()
	i.index++
	if i.err != nil {
		return
	}
	i.doInterpret()
}

func (i *Decoder) doInterpret() {
	if i.rewriteFunc != nil {
		i.file = i.File()
		if i.err != nil {
			return
		}
		var err error
		i.file, err = i.rewriteFunc(i.file)
		if err != nil {
			i.err = err
			return
		}
		// A syntax rewrite supersedes the directly decoded value.
		i.value = cue.Value{}
		i.hasValue = false
	}
	if i.interpretFunc != nil {
		var v cue.Value
		if i.hasValue {
			v = i.value
		} else {
			v = i.ctx.BuildFile(i.EvalFile())
		}
		if !i.autoInterpret || !i.hasValue {
			if err := v.Err(); err != nil {
				i.err = err
				return
			}
		}
		file, err := i.interpretFunc(v)
		if err != nil {
			i.err = err
			return
		}
		// Auto may inspect a direct bottom to decide that no interpretation
		// applies. If it did recognize one, retain the historical rule that an
		// interpreted value itself must be error-free.
		if i.autoInterpret && i.hasValue && i.interpretation != "" {
			if err := v.Err(); err != nil {
				i.err = err
				return
			}
		}
		// Auto interpretation returns no file when it detected nothing. In
		// that case retain a directly decoded value instead of forcing it
		// through the compatibility syntax projection.
		if file == nil {
			return
		}
		if i.hasValue {
			// The interpreted file becomes the current document, but callers
			// such as --with-context still need the original, pre-interpretation
			// source expression. Project it before replacing i.file.
			i.setValueSyntax()
			if i.err != nil {
				return
			}
		}
		i.file = file
		i.value = cue.Value{}
		i.hasValue = false
	}
}

// setValueSyntax creates the compatibility syntax projection used by File and
// SourceExpr. The exact decoded value remains available through DecodedValue;
// in particular, callers that need structure sharing or original conjuncts
// must use that direct path rather than rebuilding this syntax.
func (i *Decoder) setValueSyntax() {
	if i.valueProjected {
		return
	}
	i.valueProjected = true
	node := i.value.Syntax(
		cue.Definitions(true),
		cue.Hidden(true),
		cue.Optional(true),
		cue.Docs(true),
		cue.Attributes(true),
		cue.ErrorsAsValues(true),
	)
	switch x := node.(type) {
	case *ast.File:
		// Keep package and import declarations. Converting the file through
		// internal.ToExpr would discard its preamble, so File retains x.
		// SourceExpr remains an expression for legacy placement callers.
		i.file = x
		i.expr = internal.ToExpr(x)
	case ast.Expr:
		i.expr = x
	default:
		i.err = fmt.Errorf("value-plane decoder returned unexpected syntax node %T", node)
	}
}

// File returns the current document as a CUE file, as it would be written:
// the fields of a top-level struct literal become the file's declarations.
// This suits printing or rewriting the document; see [Decoder.EvalFile]
// for evaluating it.
func (i *Decoder) File() *ast.File {
	if i.hasValue && i.file == nil && i.expr == nil {
		i.setValueSyntax()
	}
	if i.file != nil {
		return i.file
	}
	if i.expr == nil {
		// No syntax is available, for example when the projection in
		// setValueSyntax failed after Done had already reported false.
		// Return an empty file rather than nil so that callers which
		// guard with Done before calling File never dereference nil;
		// the error remains observable through Err.
		return &ast.File{}
	}
	return internal.ToFile(i.expr, false)
}

// EvalFile is like [Decoder.File], but for evaluating the document rather
// than printing it. A file built from the fields of a top-level struct literal
// starts at its first field, so when the struct's opening brace is elsewhere,
// as in JSON, the struct is embedded in the file instead. This keeps the
// position for errors which point at the struct.
func (i *Decoder) EvalFile() *ast.File {
	f := i.File()
	if s, ok := i.expr.(*ast.StructLit); ok && i.file == nil &&
		s.Lbrace.IsValid() && s.Lbrace.Compare(f.Pos()) != 0 {
		return &ast.File{Decls: []ast.Decl{&ast.EmbedDecl{Expr: s}}}
	}
	return f
}

// SourceExpr returns the expression decoded from the source, before any
// interpretation such as JSON Schema is applied.
func (i *Decoder) SourceExpr() ast.Expr {
	if i.hasValue && i.file == nil && i.expr == nil {
		i.setValueSyntax()
	}
	return i.expr
}

// DecodedValue returns the current document directly when it came from a
// value-plane decoder. The returned value is the codec's original cue.Value,
// without an intervening conversion through syntax. The bool reports whether
// the direct value is available; syntax-plane decoders return false.
func (i *Decoder) DecodedValue() (cue.Value, bool) {
	return i.value, i.hasValue
}

func (i *Decoder) Err() error {
	if i.err == io.EOF {
		return nil
	}
	return i.err
}

func (i *Decoder) Close() {
	if i.closer != nil {
		i.closer.Close()
	}
}

type Config struct {
	Mode filetypes.Mode

	// Out specifies an overwrite destination.
	Out    io.Writer
	Stdin  io.Reader
	Stdout io.Writer

	PkgName string // package name for files to generate

	Force     bool // overwrite existing files
	Stream    bool // potentially write more than one document per file
	AllErrors bool

	Schema cue.Value // used for schema-based decoding

	EscapeHTML    bool
	InlineImports bool // expand references to non-core imports
	ProtoPath     []string
	Format        []format.Option
	ParserConfig  parser.Config
	ParseFile     func(name string, src interface{}, cfg parser.Config) (*ast.File, error)

	// TargetLanguageVersion holds the language version that any CUE syntax
	// generated from a non-CUE encoding must be valid at, as the result is
	// destined for a module declaring that version. The empty string means
	// the current one.
	//
	// This is deliberately not [Config.ParserConfig]'s version, which says
	// how to read CUE input: a caller may write into an older module without
	// reading any CUE at that version, and NewDecoder defaults the parser
	// version where this one is left empty on purpose.
	TargetLanguageVersion string
}

// NewDecoder returns a stream of non-rooted data expressions. The encoding
// type of f must be a data type, but does not have to be an encoding that
// can stream. stdin is used in case the file is "-".
//
// NewDecoder may set f.Source to the raw bytes it reads, so that callers
// can recover them without re-reading from disk. f must not be used
// concurrently with NewDecoder.
func NewDecoder(ctx *cue.Context, f *build.File, cfg *Config) *Decoder {
	if cfg == nil {
		cfg = &Config{}
	}
	if !cfg.ParserConfig.IsValid() {
		// Avoid mutating cfg.
		cfg.ParserConfig = parser.NewConfig(parser.ParseComments)
	}
	i := &Decoder{filename: f.Filename, ctx: ctx, cfg: cfg}
	i.next = func() (ast.Expr, error) {
		if i.err != nil {
			return nil, i.err
		}
		return nil, io.EOF
	}

	if file, ok := f.Source.(*ast.File); ok {
		i.file = file
		i.validate(file, f)
		return i
	}

	var r io.Reader
	if f.Source == nil && f.Filename == "-" {
		// TODO: should we allow this?
		r = cfg.Stdin
		i.size = -1
	} else {
		r, i.size, i.err = source.Open(f.Filename, f.Source)
		if c, ok := r.(io.Closer); ok {
			i.closer = c
		}
		if i.err != nil {
			return i
		}
	}

	switch f.Interpretation {
	case "":
	case build.Auto:
		i.autoInterpret = true
		openAPI := openAPIFunc(cfg, f)
		jsonSchema := jsonSchemaFunc(cfg, f)
		i.interpretFunc = func(v cue.Value) (file *ast.File, err error) {

			switch i.interpretation = Detect(v); i.interpretation {
			case build.JSONSchema:
				return jsonSchema(v)
			case build.OpenAPI:
				return openAPI(v)
			}
			return nil, nil
		}
	case build.OpenAPI:
		i.interpretation = build.OpenAPI
		i.interpretFunc = openAPIFunc(cfg, f)
	case build.JSONSchema:
		i.interpretation = build.JSONSchema
		i.interpretFunc = jsonSchemaFunc(cfg, f)
	case build.ProtobufJSON:
		i.interpretation = build.ProtobufJSON
		i.rewriteFunc = protobufJSONFunc(cfg, f)
	default:
		i.err = fmt.Errorf("unsupported interpretation %q", f.Interpretation)
	}

	// Binary encodings should not be treated as UTF-8, so read directly from the file.
	// Other encodings are interepted as UTF-8 with an optional BOM prefix.
	//
	// The built-in binary encoding bypasses the transform; a dynamically
	// registered encoding bypasses it when it declared itself binary (see
	// cuelang.org/go/unstable/encodingregistry). Without this a custom
	// binary decoder would receive BOM-stripped, UTF-16-transcoded, or
	// U+FFFD-replaced bytes rather than the file's actual contents.
	//
	// TODO: in the near future, others like [build.BinaryProto] should
	// also be treated as binary.
	if !isBinaryEncoding(f.Encoding) {
		// TODO: this code also allows UTF16, which is too permissive for some
		// encodings. Switch to unicode.UTF8Sig once available.
		t := unicode.BOMOverride(unicode.UTF8.NewDecoder())
		r = transform.NewReader(r, t)
	}

	path := f.Filename
	switch f.Encoding {
	case build.CUE:
		b, err := source.ReadAllSize(r, i.size)
		if err != nil {
			i.err = err
			break
		}
		// Stash the raw bytes back on the build.File so callers (such as
		// cue fix or cue trim) can compare a re-formatted file against its
		// original contents without reading the file from disk again.
		f.Source = b
		if cfg.ParseFile == nil {
			i.file, i.err = parser.ParseFile(path, b, cfg.ParserConfig)
		} else {
			i.file, i.err = cfg.ParseFile(path, b, cfg.ParserConfig)
		}
		i.validate(i.file, f)
		if i.err == nil {
			i.doInterpret()
		}
	case build.JSON:
		b, err := source.ReadAllSize(r, i.size)
		if err != nil {
			i.err = err
			break
		}
		i.expr, i.err = json.Extract(path, b)
		if i.err == nil {
			i.doInterpret()
		}
	case build.JSONL:
		i.next = json.NewDecoder(nil, path, r).Extract
		i.Next()
	case build.YAML:
		b, err := source.ReadAllSize(r, i.size)
		i.err = err
		i.next = yaml.NewDecoder(path, b).Decode
		i.Next()
	case build.TOML:
		i.next = toml.NewDecoder(path, r).Decode
		i.Next()
	case build.INI:
		i.next = ini.NewDecoder(path, r, ini.Config{}).Decode
		i.Next()
	case build.XML:
		switch {
		case f.BoolTags["koala"]:
			i.next = koala.NewDecoder(path, r).Decode
			i.Next()
		default:
			i.err = fmt.Errorf("xml requires a variant, such as: xml+koala")
		}
	case build.Text:
		b, err := source.ReadAllSize(r, i.size)
		i.err = err
		i.expr = ast.NewString(string(b))
	case build.Binary:
		b, err := source.ReadAllSize(r, i.size)
		i.err = err
		s := literal.Bytes.WithTabIndent(1).Quote(string(b))
		i.expr = ast.NewLit(token.STRING, s)
	case build.Protobuf:
		paths := &protobuf.Config{
			Paths:   cfg.ProtoPath,
			PkgName: cfg.PkgName,
		}
		i.file, i.err = protobuf.Extract(path, r, paths)
	case build.TextProto:
		b, err := source.ReadAllSize(r, i.size)
		i.err = err
		if err == nil {
			d := textproto.NewDecoder()
			i.expr, i.err = d.Parse(cfg.Schema, path, b)
		}
	default:
		// Dynamically registered encodings (see
		// cuelang.org/go/unstable/encodingregistry) are consulted after
		// the built-in cases and before the error, so built-in behavior
		// is never rerouted.
		if codec, ok := filetypes.LookupCodec(string(f.Encoding)); ok {
			if vc, ok := codec.Value.(*valuecodec.Codec); ok && vc.NewValueDecoder != nil {
				// Retain each decoded cue.Value directly. File provides a
				// compatibility syntax projection, but value-aware callers use
				// DecodedValue so no lattice information is lost.
				vdec, err := vc.NewValueDecoder(i.ctx, f, r)
				if err != nil {
					i.err = err
					break
				}
				i.nextValue = vdec.Decode
				i.Next()
				break
			}
			if codec.NewDecoder == nil {
				// The registration API requires a decoder (NewDecoder or a
				// value-plane decoder), so this is normally unreachable;
				// guard against a nil constructor rather than calling it,
				// mirroring the encoder side.
				i.err = fmt.Errorf("unsupported encoding %q", f.Encoding)
				break
			}
			dec, err := codec.NewDecoder(f, r)
			if err != nil {
				i.err = err
				break
			}
			i.next = dec.Decode
			i.Next()
			break
		}
		i.err = fmt.Errorf("unsupported encoding %q", f.Encoding)
	}

	return i
}

// isBinaryEncoding reports whether files of the encoding are read as
// raw bytes rather than normalized as UTF-8 text. Among built-ins only
// build.Binary qualifies; a dynamically registered encoding qualifies
// when it declared itself binary.
func isBinaryEncoding(enc build.Encoding) bool {
	if enc == build.Binary {
		return true
	}
	if codec, ok := filetypes.LookupCodec(string(enc)); ok {
		return codec.Binary
	}
	return false
}

func jsonSchemaFunc(cfg *Config, f *build.File) interpretFunc {
	return func(v cue.Value) (file *ast.File, err error) {
		tags := boolTagsForFile(f, build.JSONSchema)
		cfg := &jsonschema.Config{
			PkgName: cfg.PkgName,

			// Note: we don't populate Strict because then we'd
			// be ignoring the values of the other tags when it's true,
			// and there's (deliberately) nothing that Strict does that
			// cannot be described by the other two keywords.
			// The strictKeywords and strictFeatures tags are
			// set by internal/filetypes from the strict tag when appropriate.

			StrictKeywords:       tags["strictKeywords"],
			StrictFeatures:       tags["strictFeatures"],
			OpenOnlyWhenExplicit: tags["openOnlyWhenExplicit"],

			TargetLanguageVersion: cfg.TargetLanguageVersion,
		}
		file, err = jsonschema.Extract(v, cfg)
		// TODO: simplify currently erases file line info. Reintroduce after fix.
		// file, err = simplify(file, err)
		return file, err
	}
}

func openAPIFunc(c *Config, f *build.File) interpretFunc {
	return func(v cue.Value) (file *ast.File, err error) {
		tags := boolTagsForFile(f, build.JSONSchema)
		if cueexperiment.Flags.OpenAPIV2 {
			file, err = openapi.ExtractV2(v, &openapi.ExtractConfig{
				PkgName: c.PkgName,

				// Note: don't populate Strict (see more detailed
				// comment in jsonSchemaFunc)

				StrictKeywords: tags["strictKeywords"],
				StrictFeatures: tags["strictFeatures"],
			})
		} else {
			file, err = openapi.Extract(v, &openapi.Config{
				PkgName: c.PkgName,

				// Note: don't populate Strict (see more detailed
				// comment in jsonSchemaFunc)

				StrictKeywords: tags["strictKeywords"],
				StrictFeatures: tags["strictFeatures"],
			})
		}
		// TODO: simplify currently erases file line info. Reintroduce after fix.
		// file, err = simplify(file, err)
		return file, err
	}
}

func protobufJSONFunc(cfg *Config, file *build.File) rewriteFunc {
	return func(f *ast.File) (*ast.File, error) {
		if !cfg.Schema.Exists() {
			return f, errors.Newf(token.NoPos,
				"no schema specified for protobuf interpretation.")
		}
		return f, jsonpb.NewDecoder(cfg.Schema).RewriteFile(f)
	}
}

func boolTagsForFile(f *build.File, interp build.Interpretation) map[string]bool {
	if f.Interpretation != build.Auto {
		return f.BoolTags
	}
	defaultTags := filetypes.DefaultTagsForInterpretation(interp, filetypes.Input)
	if len(defaultTags) == 0 {
		return f.BoolTags
	}
	// We _could_ probably mutate f.Tags directly, but that doesn't
	// seem quite right as it's been passed in from outside of internal/encoding.
	// So go the extra mile and make a new map.

	// Set values for tags that have a default value but aren't
	// present in f.Tags.
	var tags map[string]bool
	for tag, val := range defaultTags {
		if _, ok := f.BoolTags[tag]; ok {
			continue
		}
		if tags == nil {
			tags = make(map[string]bool)
		}
		tags[tag] = val
	}
	if tags == nil {
		return f.BoolTags
	}
	maps.Copy(tags, f.BoolTags)
	return tags
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
		check(n, i.Definitions, "definitions", internal.IsDefinition(x.Label))
		check(n, i.Data, "regular fields", internal.IsRegularField(x))

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
		*ast.CallExpr, *ast.Comprehension, *ast.Interpolation:
		check(n, constraints, "expressions", true)

	case *ast.Ellipsis:
		check(n, constraints, "ellipsis", true)

	case *ast.Ident, *ast.SelectorExpr, *ast.Alias, *ast.LetClause:
		check(n, i.References, "references", true)

	default:
		// Other types are either always okay or handled elsewhere.
	}
	return ok
}
