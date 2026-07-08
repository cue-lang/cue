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
	"fmt"
	"io"
	"iter"

	"cuelang.org/go/cue/ast"

	// TODO(cleanup): drop transitive old-API dependencies when the v1
	// encoding layer is dismantled.
	"cuelang.org/go/encoding/json"
	jsonenc "cuelang.org/go/internal/encoding/json"
)

// JSON returns the built-in JSON codec. It decodes a single JSON
// document to CUE syntax and encodes a single value as JSON, and claims
// the .json extension. It is included in [Default].
func JSON() Codec { return jsonCodec{} }

type jsonCodec struct{}

func (jsonCodec) Name() string { return "json" }

func (jsonCodec) Extensions() []string { return []string{".json"} }

func (jsonCodec) NewDecoder(r io.Reader, opts *DecodeOptions) iter.Seq2[*ast.File, error] {
	return func(yield func(*ast.File, error) bool) {
		data, err := io.ReadAll(r)
		if err != nil {
			yield(nil, err)
			return
		}
		expr, err := json.Extract(filename(opts), data)
		if err != nil {
			yield(nil, err)
			return
		}
		yield(toFile(expr), nil)
	}
}

func (jsonCodec) NewEncoder(w io.Writer, opts *EncodeOptions) (EncodeStream, error) {
	return &jsonEncoder{w: w}, nil
}

// jsonEncoder encodes a single JSON document; a second Write is an
// error.
type jsonEncoder struct {
	w     io.Writer
	wrote bool
}

func (e *jsonEncoder) Write(ctx context.Context, f *ast.File) error {
	if e.wrote {
		return fmt.Errorf("json: cannot encode more than one document to a single stream")
	}
	e.wrote = true
	v, err := astToValue(f)
	if err != nil {
		return err
	}
	b, err := jsonenc.Marshal(v)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = e.w.Write(b)
	return err
}

func (e *jsonEncoder) Close() error { return nil }
