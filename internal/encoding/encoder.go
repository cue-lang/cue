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
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/encoding/ini"
	"cuelang.org/go/encoding/jsonschema"
	"cuelang.org/go/encoding/openapi"
	"cuelang.org/go/encoding/protobuf/jsonpb"
	"cuelang.org/go/encoding/protobuf/textproto"
	"cuelang.org/go/encoding/toml"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/cueexperiment"
	cueyaml "cuelang.org/go/internal/encoding/yaml"
	"cuelang.org/go/internal/filetypes"
	"cuelang.org/go/internal/valuecodec"
)

// An Encoder converts CUE to various file formats, including CUE itself.
// An Encoder allows
type Encoder struct {
	ctx          *cue.Context
	cfg          *Config
	close        func() error
	interpret    func(cue.Value) (*ast.File, error)
	encFile      func(*ast.File) error
	encValue     func(cue.Value) error
	autoSimplify bool
	concrete     bool
	valuePlane   bool
}

// IsConcrete reports whether the output is required to be concrete.
//
// INTERNAL ONLY: this is just to work around a problem related to issue #553
// of catching errors only after syntax generation, dropping line number
// information.
func (e *Encoder) IsConcrete() bool {
	return e.concrete
}

func (e Encoder) Close() error {
	if e.close == nil {
		return nil
	}
	return e.close()
}

// NewEncoder writes content to the file with the given specification.
func NewEncoder(ctx *cue.Context, f *build.File, cfg *Config) (*Encoder, error) {
	w, close := writer(f, cfg)
	e := &Encoder{
		ctx:   ctx,
		cfg:   cfg,
		close: close,
	}

	switch f.Interpretation {
	case "":
	case build.OpenAPI:
		// TODO: get encoding options
		if cueexperiment.Flags.OpenAPIV2 {
			cfg := &openapi.GenerateConfig{
				AllSchemas: f.BoolTags["allSchemas"],
			}
			e.interpret = func(v cue.Value) (*ast.File, error) {
				expr, err := openapi.GenerateV2(v, cfg)
				if err != nil {
					return nil, err
				}
				return internal.ToFile(expr, false), nil
			}
		} else {
			cfg := &openapi.Config{}
			e.interpret = func(v cue.Value) (*ast.File, error) {
				return openapi.Generate(v, cfg)
			}
		}
	case build.JSONSchema:
		// TODO: get encoding options
		cfg := &jsonschema.GenerateConfig{}
		e.interpret = func(v cue.Value) (*ast.File, error) {
			expr, err := jsonschema.Generate(v, cfg)
			if err != nil {
				return nil, err
			}
			return internal.ToFile(expr, false), nil
		}
	case build.ProtobufJSON:
		e.interpret = func(v cue.Value) (*ast.File, error) {
			f := internal.ToFile(v.Syntax(), false)
			return f, jsonpb.NewEncoder(v).RewriteFile(f)
		}
	default:
		return nil, fmt.Errorf("unsupported interpretation %q", f.Interpretation)
	}

	switch f.Encoding {
	case build.CUE:
		fi, err := filetypes.FromFile(f, cfg.Mode)
		if err != nil {
			return nil, err
		}
		e.concrete = !fi.Incomplete

		synOpts := []cue.Option{}
		if !fi.KeepDefaults || !fi.Incomplete {
			synOpts = append(synOpts, cue.Final())
		}

		synOpts = append(synOpts,
			cue.Docs(fi.Docs),
			cue.Attributes(fi.Attributes),
			cue.Optional(fi.Optional),
			cue.Concrete(!fi.Incomplete),
			cue.Definitions(fi.Definitions),
			cue.DisallowCycles(!fi.Cycles),
			cue.InlineImports(cfg.InlineImports),
		)

		opts := []format.Option{}
		opts = append(opts, cfg.Format...)
		compact := f.BoolTags["compact"]

		useSep := false
		// canMutate reports whether the encoder owns n and hence
		// whether the formatter is allowed to rewrite it in place.
		format := func(name string, n ast.Node, canMutate bool) error {
			if name != "" && cfg.Stream {
				// TODO: make this relative to DIR
				fmt.Fprintf(w, "// %s\n", filepath.Base(name))
			} else if useSep {
				fmt.Fprintln(w, "// ---")
			}
			useSep = true

			opts := opts
			if e.autoSimplify {
				opts = append(opts, format.Simplify())
			}

			// Casting an ast.Expr to an ast.File ensures that it always ends
			// with a newline. Note that this returns n unchanged when it is
			// already an *ast.File, so it does not confer ownership.
			f := internal.ToFile(n, false)
			if e.cfg.PkgName != "" && f.PackageName() == "" {
				if !canMutate {
					// Adding the package clause mutates the file, so take
					// a copy that we own before doing so.
					f = ast.Clone(f)
					canMutate = true
				}
				pkg := &ast.Package{
					PackagePos: token.NoPos.WithRel(token.NewSection),
					Name:       ast.NewIdent(e.cfg.PkgName),
				}
				doc, rest := internal.FileComments(f)
				ast.SetComments(pkg, doc)
				ast.SetComments(f, rest)
				// Attributes before the package clause are file attributes,
				// so the clause goes after any leading ones to keep them so.
				i := 0
				for i < len(f.Decls) {
					if _, ok := f.Decls[i].(*ast.Attribute); !ok {
						break
					}
					i++
				}
				f.Decls = slices.Insert(f.Decls, i, ast.Decl(pkg))
			}
			if compact {
				opts = append(opts, format.Compact())
			}
			var b []byte
			var err error
			if canMutate {
				b, err = format.NodeInPlace(f, opts...)
			} else {
				b, err = format.Node(f, opts...)
			}
			if err != nil {
				return err
			}
			_, err = w.Write(b)
			return err
		}
		e.encValue = func(v cue.Value) error {
			// The syntax is freshly built here, so the formatter may
			// rewrite it in place.
			return format("", v.Syntax(synOpts...), true)
		}
		// The file belongs to the caller of [Encoder.EncodeFile], so it
		// must not be mutated.
		e.encFile = func(f *ast.File) error { return format(f.Filename, f, false) }

	case build.JSON, build.JSONL:
		e.concrete = true
		d := json.NewEncoder(w)
		if !f.BoolTags["compact"] {
			d.SetIndent("", "    ")
		}
		d.SetEscapeHTML(cfg.EscapeHTML)
		e.encValue = func(v cue.Value) error {
			err := d.Encode(v)
			if x, ok := err.(*json.MarshalerError); ok {
				err = x.Err
			}
			return err
		}

	case build.YAML:
		e.concrete = true
		streamed := false
		indentSeq := f.BoolTags["indentSequences"]
		// TODO(mvdan): use a NewEncoder API like in TOML below.
		e.encValue = func(v cue.Value) error {
			if streamed {
				fmt.Fprintln(w, "---")
			}
			streamed = true

			// Note that we use [cue.Concrete] here, which expands all references.
			n := v.Syntax(cue.Concrete(true))
			b, err := cueyaml.Encode(n, cueyaml.IndentSequences(indentSeq))
			if err != nil {
				return err
			}
			_, err = w.Write(b)
			return err
		}

	case build.TOML:
		e.concrete = true
		enc := toml.NewEncoder(w)
		e.encValue = enc.Encode

	case build.INI:
		e.concrete = true
		e.encValue = ini.NewEncoder(w, iniConfig(f)).Encode

	case build.TextProto:
		// TODO: verify that the schema is given. Otherwise err out.
		e.concrete = true
		e.encValue = func(v cue.Value) error {
			v = v.Unify(cfg.Schema)
			b, err := textproto.NewEncoder().Encode(v)
			if err != nil {
				return err
			}

			_, err = w.Write(b)
			return err
		}

	case build.Text:
		e.concrete = true
		e.encValue = func(v cue.Value) error {
			s, err := v.String()
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(w, s)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintln(w)
			return err
		}

	case build.Binary:
		e.concrete = true
		e.encValue = func(v cue.Value) error {
			b, err := v.Bytes()
			if err != nil {
				return err
			}
			_, err = w.Write(b)
			return err
		}

	default:
		// Dynamically registered encodings (see
		// cuelang.org/go/unstable/encodingregistry) are consulted after
		// the built-in cases and before the error. A registration
		// without an encoder is decode-only and yields the same error
		// an unencodable built-in produces.
		codec, ok := filetypes.LookupCodec(string(f.Encoding))
		if !ok {
			return nil, fmt.Errorf("unsupported encoding %q", f.Encoding)
		}
		// Both planes derive concreteness — and the syntax plane its
		// projection options — from the resolved file info, exactly as
		// the built-in CUE branch does, so a registration's declared
		// form and aspects take effect.
		fi, err := filetypes.FromFile(f, cfg.Mode)
		if err != nil {
			return nil, err
		}
		e.concrete = !fi.Incomplete
		var closeCodec func() error
		if vc, ok := codec.Value.(*valuecodec.Codec); ok && vc.NewValueEncoder != nil {
			// Value-plane encoder: pass the raw cue.Value, preserving the
			// lattice, and enforce concreteness only when the mode
			// requires it (the resolved form's Incomplete aspect),
			// mirroring the built-in CUE and wire encoders.
			venc, err := vc.NewValueEncoder(f, w)
			if err != nil {
				return nil, err
			}
			e.valuePlane = true
			e.encValue = venc.Encode
			closeCodec = venc.Close
		} else {
			if codec.NewEncoder == nil {
				return nil, fmt.Errorf("unsupported encoding %q", f.Encoding)
			}
			enc, err := codec.NewEncoder(f, w)
			if err != nil {
				return nil, err
			}
			// Mirror the built-in CUE branch's syntax options so the
			// codec receives what the registration declared: docs,
			// attributes, optional fields, definitions, and mode-driven
			// concreteness, rather than an unconditionally concrete
			// projection.
			synOpts := []cue.Option{}
			if !fi.KeepDefaults || !fi.Incomplete {
				synOpts = append(synOpts, cue.Final())
			}
			synOpts = append(synOpts,
				cue.Docs(fi.Docs),
				cue.Attributes(fi.Attributes),
				cue.Optional(fi.Optional),
				cue.Concrete(!fi.Incomplete),
				cue.Definitions(fi.Definitions),
				cue.DisallowCycles(!fi.Cycles),
				cue.InlineImports(cfg.InlineImports),
			)
			e.encValue = func(v cue.Value) error {
				return enc.Encode(v.Syntax(synOpts...))
			}
			closeCodec = enc.Close
		}
		wclose := e.close
		e.close = func() error {
			return closeAndCommit(closeCodec, wclose)
		}
	}

	return e, nil
}

func (e *Encoder) EncodeFile(f *ast.File) error {
	if e.interpret == nil && e.encFile != nil {
		// TODO it's not clear that it's actually desirable to turn
		// off simplification in this case. This case generally arises
		// when we're producing CUE code with `cue eval` and
		// simplified results seem generally preferable.
		e.autoSimplify = false
		return e.encFile(f)
	}
	e.autoSimplify = true
	return e.Encode(e.ctx.BuildFile(f))
}

func (e *Encoder) Encode(v cue.Value) error {
	e.autoSimplify = true
	if e.interpret == nil {
		// An incomplete value-plane format carries the full lattice,
		// including bottom values. Let the codec serialize that value as-is.
		// Concrete modes retain the usual validation requirement.
		if !e.valuePlane || e.concrete {
			if err := v.Validate(cue.Concrete(e.concrete)); err != nil {
				return err
			}
		}
		return e.encValue(v)
	}
	if err := v.Validate(); err != nil {
		return err
	}
	f, err := e.interpret(v)
	if err != nil {
		return err
	}
	if e.encFile != nil {
		return e.encFile(f)
	}
	v = e.ctx.BuildFile(f)
	if err := v.Validate(cue.Concrete(e.concrete)); err != nil {
		return err
	}
	return e.encValue(v)
}

// closeAndCommit finishes a codec before publishing bytes buffered by writer.
// A codec close error means its output is incomplete and must never replace the
// destination file.
func closeAndCommit(closeCodec, commit func() error) error {
	if err := closeCodec(); err != nil {
		return err
	}
	if commit != nil {
		return commit()
	}
	return nil
}

func writer(f *build.File, cfg *Config) (_ io.Writer, close func() error) {
	if cfg.Out != nil {
		return cfg.Out, nil
	}
	path := f.Filename
	if path == "-" {
		if cfg.Stdout == nil {
			return os.Stdout, nil
		}
		return cfg.Stdout, nil
	}
	// Delay opening the file until we can write it to completion.
	// This prevents clobbering the file in case of a crash.
	b := &bytes.Buffer{}
	fn := func() error {
		mode := os.O_WRONLY | os.O_CREATE | os.O_EXCL
		if cfg.Force {
			// Swap O_EXCL for O_TRUNC to allow replacing an entire existing file.
			mode = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
		}
		f, err := os.OpenFile(path, mode, 0o666)
		if errors.Is(err, fs.ErrExist) {
			// If we failed because the file already existed,
			// but the file in question is not regular, allow writing to it.
			// This is done as a retry to avoid a Stat call before every OpenFile.
			stat, err2 := os.Stat(path)
			if err2 == nil && !stat.Mode().IsRegular() {
				f, err = os.OpenFile(path, os.O_WRONLY, 0o666)
			} else {
				return errors.Wrapf(fs.ErrExist, token.NoPos, "error writing %q", path)
			}
		}
		if err != nil {
			return err
		}
		_, err = f.Write(b.Bytes())
		if err1 := f.Close(); err1 != nil && err == nil {
			err = err1
		}
		return err
	}
	return b, fn
}
