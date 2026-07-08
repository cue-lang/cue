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

package cuecodec

import (
	"context"
	"io"
	"iter"

	"cuelang.org/go/cue/ast"

	// TODO(cleanup): drop transitive old-API dependencies when the v1
	// encoding layer is dismantled.
	"cuelang.org/go/internal/encoding/yaml"
)

// YAML returns the built-in YAML codec. It decodes a YAML stream into
// one CUE document per YAML document and encodes a sequence of values as
// a multi-document YAML stream, and claims the .yaml and .yml
// extensions. It is included in [Default].
func YAML() Codec { return yamlCodec{} }

type yamlCodec struct{}

func (yamlCodec) Name() string { return "yaml" }

func (yamlCodec) Extensions() []string { return []string{".yaml", ".yml"} }

func (yamlCodec) NewDecoder(r io.Reader, opts *DecodeOptions) iter.Seq2[*ast.File, error] {
	return func(yield func(*ast.File, error) bool) {
		data, err := io.ReadAll(r)
		if err != nil {
			yield(nil, err)
			return
		}
		dec := yaml.NewDecoder(filename(opts), data)
		for {
			expr, err := dec.Decode()
			if err == io.EOF {
				return
			}
			if err != nil {
				yield(nil, err)
				return
			}
			if !yield(toFile(expr), nil) {
				return
			}
		}
	}
}

func (yamlCodec) NewEncoder(w io.Writer, opts *EncodeOptions) (EncodeStream, error) {
	return &yamlEncoder{w: w}, nil
}

// yamlEncoder encodes a multi-document YAML stream, separating documents
// with a "---" line.
type yamlEncoder struct {
	w     io.Writer
	wrote bool
}

func (e *yamlEncoder) Write(ctx context.Context, f *ast.File) error {
	b, err := yaml.Encode(f)
	if err != nil {
		return err
	}
	if e.wrote {
		if _, err := io.WriteString(e.w, "---\n"); err != nil {
			return err
		}
	}
	e.wrote = true
	_, err = e.w.Write(b)
	return err
}

func (e *yamlEncoder) Close() error { return nil }
