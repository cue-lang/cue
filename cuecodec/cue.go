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
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
)

// CUE returns the built-in CUE codec. It parses CUE source into syntax
// and formats syntax back to CUE, and claims the .cue extension. It is
// included in [Default].
func CUE() Codec { return cueCodec{} }

type cueCodec struct{}

func (cueCodec) Name() string { return "cue" }

func (cueCodec) Extensions() []string { return []string{".cue"} }

func (cueCodec) NewDecoder(r io.Reader, opts *DecodeOptions) iter.Seq2[*ast.File, error] {
	return func(yield func(*ast.File, error) bool) {
		data, err := io.ReadAll(r)
		if err != nil {
			yield(nil, err)
			return
		}
		f, err := parser.ParseFile(filename(opts), data, parser.ParseComments)
		if err != nil {
			yield(nil, err)
			return
		}
		yield(f, nil)
	}
}

func (cueCodec) NewEncoder(w io.Writer, opts *EncodeOptions) (EncodeStream, error) {
	return &cueEncoder{w: w}, nil
}

// cueEncoder concatenates encoded files, separating them with a blank
// line.
type cueEncoder struct {
	w     io.Writer
	wrote bool
}

func (e *cueEncoder) Write(ctx context.Context, f *ast.File) error {
	b, err := format.Node(f)
	if err != nil {
		return err
	}
	if e.wrote {
		if _, err := io.WriteString(e.w, "\n"); err != nil {
			return err
		}
	}
	e.wrote = true
	_, err = e.w.Write(b)
	return err
}

func (e *cueEncoder) Close() error { return nil }

func filename(opts *DecodeOptions) string {
	if opts == nil {
		return ""
	}
	return opts.Filename
}
