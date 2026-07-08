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

package cuecodec_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cuecodec"
)

// decodeAll decodes src with c, returning every document or failing on
// the first error.
func decodeAll(t *testing.T, c cuecodec.Codec, src string, opts *cuecodec.DecodeOptions) []*ast.File {
	t.Helper()
	dec, ok := c.(cuecodec.Decoder)
	qt.Assert(t, qt.IsTrue(ok))
	var files []*ast.File
	for f, err := range dec.NewDecoder(strings.NewReader(src), opts) {
		qt.Assert(t, qt.IsNil(err))
		files = append(files, f)
	}
	return files
}

// encodeAll encodes files with c and returns the result.
func encodeAll(t *testing.T, c cuecodec.Codec, opts *cuecodec.EncodeOptions, files ...*ast.File) string {
	t.Helper()
	enc, ok := c.(cuecodec.Encoder)
	qt.Assert(t, qt.IsTrue(ok))
	var buf bytes.Buffer
	es, err := enc.NewEncoder(&buf, opts)
	qt.Assert(t, qt.IsNil(err))
	for _, f := range files {
		qt.Assert(t, qt.IsNil(es.Write(context.Background(), f)))
	}
	qt.Assert(t, qt.IsNil(es.Close()))
	return buf.String()
}

func TestCUERoundTrip(t *testing.T) {
	const src = "a: 1\nb: \"two\"\nc: [1, 2, 3]\n"
	files := decodeAll(t, cuecodec.CUE(), src, nil)
	qt.Assert(t, qt.Equals(len(files), 1))
	got := encodeAll(t, cuecodec.CUE(), nil, files...)
	qt.Assert(t, qt.Equals(got, src))
}

func TestCUEConcat(t *testing.T) {
	f1 := decodeAll(t, cuecodec.CUE(), "a: 1\n", nil)
	f2 := decodeAll(t, cuecodec.CUE(), "b: 2\n", nil)
	got := encodeAll(t, cuecodec.CUE(), nil, f1[0], f2[0])
	qt.Assert(t, qt.Equals(got, "a: 1\n\nb: 2\n"))
}

func TestJSONRoundTrip(t *testing.T) {
	const src = `{"age":30,"name":"bob","tags":["a","b"]}`
	files := decodeAll(t, cuecodec.JSON(), src, nil)
	qt.Assert(t, qt.Equals(len(files), 1))
	got := encodeAll(t, cuecodec.JSON(), nil, files...)
	qt.Assert(t, qt.Equals(got, `{"age":30,"name":"bob","tags":["a","b"]}`+"\n"))
}

func TestJSONToYAML(t *testing.T) {
	files := decodeAll(t, cuecodec.JSON(), `{"age":30,"name":"bob"}`, nil)
	got := encodeAll(t, cuecodec.YAML(), nil, files...)
	qt.Assert(t, qt.Equals(got, "age: 30\nname: bob\n"))
}

func TestYAMLMultiDoc(t *testing.T) {
	const src = "name: bob\nage: 30\n---\nname: alice\nage: 25\n"
	files := decodeAll(t, cuecodec.YAML(), src, nil)
	qt.Assert(t, qt.Equals(len(files), 2))
	got := encodeAll(t, cuecodec.YAML(), nil, files...)
	qt.Assert(t, qt.Equals(got, src))
}

func TestNilOptsDecode(t *testing.T) {
	files := decodeAll(t, cuecodec.JSON(), `{"a":1}`, nil)
	qt.Assert(t, qt.Equals(len(files), 1))
	qt.Assert(t, qt.IsNotNil(files[0]))
}

func TestFilenameOption(t *testing.T) {
	// A syntax error should report the configured filename.
	dec := cuecodec.JSON().(cuecodec.Decoder)
	var gotErr error
	for _, err := range dec.NewDecoder(strings.NewReader("{bad"), &cuecodec.DecodeOptions{Filename: "my.json"}) {
		gotErr = err
	}
	qt.Assert(t, qt.IsNotNil(gotErr))
	qt.Assert(t, qt.StringContains(gotErr.Error(), "my.json"))
}

func TestSecondWriteRejection(t *testing.T) {
	files := decodeAll(t, cuecodec.JSON(), `{"a":1}`, nil)
	enc := cuecodec.JSON().(cuecodec.Encoder)
	var buf bytes.Buffer
	es, err := enc.NewEncoder(&buf, nil)
	qt.Assert(t, qt.IsNil(err))
	qt.Assert(t, qt.IsNil(es.Write(context.Background(), files[0])))
	err = es.Write(context.Background(), files[0])
	qt.Assert(t, qt.IsNotNil(err))
	qt.Assert(t, qt.StringContains(err.Error(), "more than one"))
}

func TestErrorPropagation(t *testing.T) {
	cases := []struct {
		name  string
		codec cuecodec.Codec
		src   string
	}{
		{"json", cuecodec.JSON(), "{not json"},
		{"yaml", cuecodec.YAML(), "a: b: c\n"},
		{"cue", cuecodec.CUE(), "a: ]["},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dec := tc.codec.(cuecodec.Decoder)
			var (
				gotErr error
				n      int
			)
			for f, err := range dec.NewDecoder(strings.NewReader(tc.src), nil) {
				n++
				if err != nil {
					gotErr = err
					qt.Assert(t, qt.IsNil(f))
				}
			}
			qt.Assert(t, qt.IsNotNil(gotErr))
			// On error the sequence yields exactly one (nil, err) pair.
			qt.Assert(t, qt.Equals(n, 1))
		})
	}
}

func TestDefaultSet(t *testing.T) {
	s := cuecodec.Default()
	for _, name := range []string{"cue", "json", "yaml"} {
		e, ok := s.Lookup(name)
		qt.Assert(t, qt.IsTrue(ok))
		qt.Assert(t, qt.Equals(e.Name(), name))
	}
	_, ok := s.Lookup("toml")
	qt.Assert(t, qt.IsFalse(ok))
}

func TestExtensionResolution(t *testing.T) {
	s := cuecodec.Default()
	cases := []struct {
		ext  string
		name string
	}{
		{".cue", "cue"},
		{".json", "json"},
		{".yaml", "yaml"},
		{".yml", "yaml"},
		{"json", "json"}, // leading dot optional
	}
	for _, tc := range cases {
		c, ok := s.ByExtension(tc.ext)
		qt.Assert(t, qt.IsTrue(ok))
		qt.Assert(t, qt.Equals(c.Name(), tc.name))
	}
	_, ok := s.ByExtension(".toml")
	qt.Assert(t, qt.IsFalse(ok))
}

// fakeCodec is a minimal Codec used to test Set replacement.
type fakeCodec struct {
	name string
	exts []string
}

func (c fakeCodec) Name() string         { return c.name }
func (c fakeCodec) Extensions() []string { return c.exts }

func TestSetWithReplacement(t *testing.T) {
	s := cuecodec.Default()
	replacement := fakeCodec{name: "json", exts: []string{".jsn"}}
	s2 := s.With(replacement)

	// Original set is unchanged (immutability).
	orig, ok := s.Lookup("json")
	qt.Assert(t, qt.IsTrue(ok))
	_, isFake := orig.(fakeCodec)
	qt.Assert(t, qt.IsFalse(isFake))

	// The new set holds the replacement under the same name.
	got, ok := s2.Lookup("json")
	qt.Assert(t, qt.IsTrue(ok))
	_, isFake = got.(fakeCodec)
	qt.Assert(t, qt.IsTrue(isFake))

	// Its new extension resolves, and the old one no longer does.
	c, ok := s2.ByExtension(".jsn")
	qt.Assert(t, qt.IsTrue(ok))
	qt.Assert(t, qt.Equals(c.Name(), "json"))
	_, ok = s2.ByExtension(".json")
	qt.Assert(t, qt.IsFalse(ok))
}

func TestSetWithAppend(t *testing.T) {
	extra := fakeCodec{name: "xml", exts: []string{".xml"}}
	s := cuecodec.Default().With(extra)
	e, ok := s.Lookup("xml")
	qt.Assert(t, qt.IsTrue(ok))
	qt.Assert(t, qt.Equals(e.Name(), "xml"))
	c, ok := s.ByExtension(".xml")
	qt.Assert(t, qt.IsTrue(ok))
	qt.Assert(t, qt.Equals(c.Name(), "xml"))
}
