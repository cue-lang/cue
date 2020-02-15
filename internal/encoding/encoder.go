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
	"encoding/json"
	"fmt"
	"io"
	"os"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/pkg/encoding/yaml"
)

// file.Format
// file.Info

// An Encoder converts CUE to various file formats, including CUE itself.
// An Encoder allows
type Encoder struct {
	cfg       *Config
	closer    io.Closer
	interpret func(cue.Value) (*ast.File, error)
	encFile   func(*ast.File) error
	encValue  func(cue.Value) error
	encInst   func(*cue.Instance) error
}

func (e Encoder) Close() error {
	if e.closer == nil {
		return nil
	}
	return e.closer.Close()
}

// NewEncoder writes content to the file with the given specification.
func NewEncoder(f *build.File, cfg *Config) (*Encoder, error) {
	w, closer, err := writer(f, cfg)
	if err != nil {
		return nil, err
	}
	e := &Encoder{
		cfg:    cfg,
		closer: closer,
	}

	switch f.Interpretation {
	case "":
	// case build.OpenAPI:
	// 	// TODO: get encoding options
	// 	cfg := openapi.Config{}
	// 	i.interpret = func(inst *cue.Instance) (*ast.File, error) {
	// 		return openapi.Generate(inst, cfg)
	// 	}
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
	case build.JSON, build.JSONL:
		// SetEscapeHTML
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
		streamed := false
		e.encValue = func(v cue.Value) error {
			if streamed {
				fmt.Fprintln(w, "---")
			}
			streamed = true

			str, err := yaml.Marshal(v)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(w, str)
			return err
		}

	case build.Text:
		e.encValue = func(v cue.Value) error {
			str, err := v.String()
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(w, str)
			return err
		}

	default:
		return nil, fmt.Errorf("unsupported encoding %q", f.Encoding)
	}

	return e, nil
}

func (e *Encoder) EncodeFile(f *ast.File) error {
	return e.encodeFile(f, e.interpret)
}

func (e *Encoder) EncodeExpr(x ast.Expr) error {
	return e.EncodeFile(toFile(x))
}

func (e *Encoder) Encode(v cue.Value) error {
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
	return e.encFile(valueToFile(v))
}

func (e *Encoder) encodeFile(f *ast.File, interpret func(cue.Value) (*ast.File, error)) error {
	if interpret == nil && e.encFile != nil {
		return e.encFile(f)
	}
	var r cue.Runtime
	inst, err := r.CompileFile(f)
	if err != nil {
		return err
	}
	if interpret != nil {
		return e.Encode(inst.Value())
	}
	return e.encValue(inst.Value())
}

func writer(f *build.File, cfg *Config) (io.Writer, io.Closer, error) {
	if cfg.Out != nil {
		return cfg.Out, nil, nil
	}
	path := f.Filename
	if path == "-" {
		if cfg.Stdout == nil {
			return os.Stdout, nil, nil
		}
		return cfg.Stdout, nil, nil
	}
	if !cfg.Force {
		if _, err := os.Stat(path); err == nil {
			return nil, nil, errors.Wrapf(os.ErrExist, token.NoPos,
				"error writing %q", path)
		}
	}
	w, err := os.Create(path)
	return w, w, err
}
