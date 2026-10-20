// Copyright 2026 CUE Authors
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

package cmd_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	"cuelang.org/go/cmd/cue/cmd"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/encoding/json"
	"cuelang.org/go/internal/filetypes"
	"github.com/go-quicktest/qt"
)

// jsonDocDecoder is a syntax-plane decoder for a synthetic registered
// encoding whose payload is a single JSON document.
type jsonDocDecoder struct {
	filename string
	r        io.Reader
	done     bool
}

func newJSONDocDecoder(f *build.File, r io.Reader) (filetypes.Decoder, error) {
	return &jsonDocDecoder{filename: f.Filename, r: r}, nil
}

func (d *jsonDocDecoder) Decode() (ast.Expr, error) {
	if d.done {
		return nil, io.EOF
	}
	d.done = true
	b, err := io.ReadAll(d.r)
	if err != nil {
		return nil, err
	}
	return json.Extract(d.filename, b)
}

// TestDynamicEncodingProtobufJSONSchema verifies that a dynamically
// registered encoding declaring the "pb" interpretation takes the same
// delayed-decoder path as the built-in data encodings, so that the
// schema determined by parseArgs reaches its decoder. Without the
// delay, the decoder is created from a config copy taken before the
// schema is known and fails with "no schema specified for protobuf
// interpretation."
func TestDynamicEncodingProtobufJSONSchema(t *testing.T) {
	filetypes.ResetDynamicRegistryForTesting()
	t.Cleanup(filetypes.ResetDynamicRegistryForTesting)

	err := filetypes.RegisterEncoding(filetypes.DynamicEncoding{
		Name:       "zzpb",
		Extensions: []string{".zzpb"},
		Info:       filetypes.DynamicFileInfo{Interpretation: "pb"},
		Codec: filetypes.Codec{
			NewDecoder: newJSONDocDecoder,
		},
	})
	qt.Assert(t, qt.IsNil(err))

	dir := t.TempDir()
	writeFile := func(name, content string) {
		t.Helper()
		err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o666)
		qt.Assert(t, qt.IsNil(err))
	}
	writeFile("sch.cue", `package p

foo_bar: int @protobuf(1,int64)
`)
	// Per the protobuf JSON mapping an int64 arrives as a JSON string;
	// only a decoder that received the schema converts it to a number.
	writeFile("data.zzpb", `{"foo_bar": "42"}`)
	writeFile("data.json", `{"foo_bar": "42"}`)
	t.Chdir(dir)

	run := func(t *testing.T, args ...string) string {
		t.Helper()
		c, err := cmd.New(args)
		qt.Assert(t, qt.IsNil(err))
		var stdout, stderr bytes.Buffer
		c.SetOut(&stdout)
		c.SetErr(&stderr)
		err = c.Run(t.Context())
		qt.Assert(t, qt.IsNil(err), qt.Commentf("stderr: %s", stderr.String()))
		return stdout.String()
	}

	const want = "{\n    \"foo_bar\": 42\n}\n"

	// Control: the built-in json encoding with the pb interpretation.
	got := run(t, "export", "sch.cue", "json+pb:", "data.json")
	qt.Assert(t, qt.Equals(got, want))

	// The registered encoding must behave identically.
	got = run(t, "export", "sch.cue", "data.zzpb")
	qt.Assert(t, qt.Equals(got, want))
}
