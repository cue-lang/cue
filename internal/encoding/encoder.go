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

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/encoding/openapi"
	"cuelang.org/go/encoding/protobuf/jsonpb"
	"cuelang.org/go/encoding/protobuf/textproto"
	"cuelang.org/go/encoding/toml"
	"cuelang.org/go/encoding/yaml"
	"cuelang.org/go/internal"
	"cuelang.org/go/internal/filetypes"
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
		cfg := &openapi.Config{}
		e.interpret = func(v cue.Value) (*ast.File, error) {
			return openapi.Generate(v, cfg)
		}
	case build.ProtobufJSON:
		e.interpret = func(v cue.Value) (*ast.File, error) {
			f := internal.ToFile(v.Syntax())
			return f, jsonpb.NewEncoder(v).RewriteFile(f)
		}

	// case build.JSONSchema:
	// 	// TODO: get encoding options
	// 	cfg := openapi.Config{}
	// 	i.interpret = func(inst *cue.Instance) (*ast.File, error) {
	// 		return jsonschmea.Generate(inst, cfg)
	// 	}
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

		useSep := false
		format := func(name string, n ast.Node) error {
			if name != "" && cfg.Stream {
				// TODO: make this relative to DIR
				fmt.Fprintf(w, "// %s\n", filepath.Base(name))
			} else if useSep {
				fmt.Println("// ---")
			}
			useSep = true

			opts := opts
			if e.autoSimplify {
				opts = append(opts, format.Simplify())
			}

			// Casting an ast.Expr to an ast.File ensures that it always ends
			// with a newline.
			f := internal.ToFile(n)
			if e.cfg.PkgName != "" && f.PackageName() == "" {
				f.Decls = append([]ast.Decl{
					&ast.Package{
						Name: ast.NewIdent(e.cfg.PkgName),
					},
				}, f.Decls...)
			}
			b, err := format.Node(f, opts...)
			if err != nil {
				return err
			}
			_, err = w.Write(b)
			return err
		}
		e.encValue = func(v cue.Value) error {
			return format("", v.Syntax(synOpts...))
		}
		e.encFile = func(f *ast.File) error { return format(f.Filename, f) }

	case build.JSON, build.JSONL:
		e.concrete = true
		d := json.NewEncoder(w)
		d.SetIndent("", "    ")
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
		// TODO(mvdan): use a NewEncoder API like in TOML below.
		e.encValue = func(v cue.Value) error {
			if streamed {
				fmt.Fprintln(w, "---")
			}
			streamed = true

			b, err := yaml.Encode(v)
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
		return nil, fmt.Errorf("unsupported encoding %q", f.Encoding)
	}

	return e, nil
}

func (e *Encoder) EncodeFile(f *ast.File) error {
	e.autoSimplify = false
	return e.encodeFile(f, e.interpret)
}

func (e *Encoder) Encode(v cue.Value) error {
	e.autoSimplify = true
	if err := v.Validate(cue.Concrete(e.concrete)); err != nil {
		return err
	}
	if e.interpret != nil {
		f, err := e.interpret(v)
		if err != nil {
			return err
		}
		return e.encodeFile(f, nil)
	}
	if e.encValue != nil {
		return e.encValue(v)
	}
	return e.encFile(internal.ToFile(v.Syntax()))
}

func (e *Encoder) encodeFile(f *ast.File, interpret func(cue.Value) (*ast.File, error)) error {
	if interpret == nil && e.encFile != nil {
		return e.encFile(f)
	}
	e.autoSimplify = true
	v := e.ctx.BuildFile(f)
	if err := v.Err(); err != nil {
		return err
	}
	if interpret != nil {
		return e.Encode(v)
	}
	if err := v.Validate(cue.Concrete(e.concrete)); err != nil {
		return err
	}
	return e.encValue(v)
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
		if err != nil {
			if errors.Is(err, fs.ErrExist) {
				return errors.Wrapf(fs.ErrExist, token.NoPos, "error writing %q", path)
			}
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
