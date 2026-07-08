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

// Package toml provides the TOML codec for CUE, decoding TOML documents
// to CUE syntax and encoding concrete CUE values as TOML. It exists as a
// separate package so that programs not using TOML do not link its
// dependencies.
//
// Add it to a loader with:
//
//	cfg.Codecs = cuecodec.Default().With(toml.Codec())
package toml

import (
	"context"
	"fmt"
	"io"
	"iter"
	"strings"

	gotoml "github.com/pelletier/go-toml/v2"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cuecodec"

	// TODO(cleanup): drop transitive old-API dependencies when the v1
	// encoding layer is dismantled.
	tomldec "cuelang.org/go/encoding/toml"
)

// Codec returns the TOML codec. It implements [cuecodec.Decoder] and
// [cuecodec.Encoder], and claims the .toml extension.
func Codec() cuecodec.Codec { return tomlCodec{} }

type tomlCodec struct{}

func (tomlCodec) Name() string { return "toml" }

func (tomlCodec) Extensions() []string { return []string{".toml"} }

func (tomlCodec) NewDecoder(r io.Reader, opts *cuecodec.DecodeOptions) iter.Seq2[*ast.File, error] {
	return func(yield func(*ast.File, error) bool) {
		name := ""
		if opts != nil {
			name = opts.Filename
		}
		expr, err := tomldec.NewDecoder(name, r).Decode()
		if err != nil {
			yield(nil, err)
			return
		}
		yield(toFile(expr), nil)
	}
}

func (tomlCodec) NewEncoder(w io.Writer, opts *cuecodec.EncodeOptions) (cuecodec.EncodeStream, error) {
	e := &tomlEncoder{w: w}
	if opts != nil {
		for _, o := range opts.Options {
			if ind, ok := o.(indentOption); ok {
				e.indent = ind.n
				e.hasIndent = true
			}
		}
	}
	return e, nil
}

// tomlEncoder encodes a single TOML document; a second Write is an
// error, as TOML has no multi-document form.
type tomlEncoder struct {
	w         io.Writer
	indent    int
	hasIndent bool
	wrote     bool
}

func (e *tomlEncoder) Write(ctx context.Context, f *ast.File) error {
	if e.wrote {
		return fmt.Errorf("toml: cannot encode more than one document to a single stream")
	}
	e.wrote = true
	v, err := astToValue(f)
	if err != nil {
		return err
	}
	enc := gotoml.NewEncoder(e.w)
	if e.hasIndent {
		enc.SetIndentTables(true)
		enc.SetIndentSymbol(strings.Repeat(" ", e.indent))
	}
	return enc.Encode(v)
}

func (e *tomlEncoder) Close() error { return nil }

// Indent returns an option controlling the indentation of emitted TOML
// (an example of a codec-specific option). It sets the number of spaces
// used to indent nested tables.
func Indent(n int) cuecodec.Option { return indentOption{n: n} }

type indentOption struct {
	cuecodec.OptionBase
	n int
}
