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

package toml_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cuecodec"
	"cuelang.org/go/cuecodec/toml"
)

func decodeAll(t *testing.T, src string, opts *cuecodec.DecodeOptions) []*ast.File {
	t.Helper()
	dec := toml.Codec().(cuecodec.Decoder)
	var files []*ast.File
	for f, err := range dec.NewDecoder(strings.NewReader(src), opts) {
		qt.Assert(t, qt.IsNil(err))
		files = append(files, f)
	}
	return files
}

func encode(t *testing.T, opts *cuecodec.EncodeOptions, f *ast.File) string {
	t.Helper()
	enc := toml.Codec().(cuecodec.Encoder)
	var buf bytes.Buffer
	es, err := enc.NewEncoder(&buf, opts)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsNil(es.Write(context.Background(), f)))
	qt.Assert(t, qt.IsNil(es.Close()))
	return buf.String()
}

func TestCodecMetadata(t *testing.T) {
	c := toml.Codec()
	qt.Assert(t, qt.Equals(c.Name(), "toml"))
	qt.Assert(t, qt.DeepEquals(c.Extensions(), []string{".toml"}))
}

func TestRoundTrip(t *testing.T) {
	const src = "age = 30\nname = 'bob'\n"
	files := decodeAll(t, src, nil)
	qt.Assert(t, qt.Equals(len(files), 1))
	got := encode(t, nil, files[0])
	qt.Assert(t, qt.Equals(got, src))
}

func TestNestedTable(t *testing.T) {
	files := decodeAll(t, "[server]\nhost = 'x'\nport = 8080\n", nil)
	got := encode(t, nil, files[0])
	qt.Assert(t, qt.Equals(got, "[server]\nhost = 'x'\nport = 8080\n"))
}

func TestIndentOption(t *testing.T) {
	files := decodeAll(t, "[server]\nhost = 'x'\nport = 8080\n", nil)
	opts := &cuecodec.EncodeOptions{Options: []cuecodec.Option{toml.Indent(4)}}
	got := encode(t, opts, files[0])
	qt.Assert(t, qt.Equals(got, "[server]\n    host = 'x'\n    port = 8080\n"))
}

func TestSecondWriteRejection(t *testing.T) {
	files := decodeAll(t, "a = 1\n", nil)
	enc := toml.Codec().(cuecodec.Encoder)
	var buf bytes.Buffer
	es, err := enc.NewEncoder(&buf, nil)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsNil(es.Write(context.Background(), files[0])))
	err = es.Write(context.Background(), files[0])
	qt.Assert(t, qt.IsNotNil(err))
	qt.Assert(t, qt.StringContains(err.Error(), "more than one"))
}

func TestErrorPropagation(t *testing.T) {
	dec := toml.Codec().(cuecodec.Decoder)
	var (
		gotErr error
		n      int
	)
	for f, err := range dec.NewDecoder(strings.NewReader("a = = 1"), nil) {
		n++
		if err != nil {
			gotErr = err
			qt.Assert(t, qt.IsNil(f))
		}
	}
	qt.Assert(t, qt.IsNotNil(gotErr))
	qt.Assert(t, qt.Equals(n, 1))
}

func TestWithDefaultSet(t *testing.T) {
	s := cuecodec.Default().With(toml.Codec())
	c, ok := s.ByExtension(".toml")
	qt.Assert(t, qt.IsTrue(ok))
	qt.Assert(t, qt.Equals(c.Name(), "toml"))
	e, ok := s.Lookup("toml")
	qt.Assert(t, qt.IsTrue(ok))
	qt.Assert(t, qt.Equals(e.Name(), "toml"))
}
