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

package encoding_test

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/encoding"
	"cuelang.org/go/internal/filetypes"
)

// TestEncoderClose checks that a file output is only written once the
// codec closes successfully.
func TestEncoderClose(t *testing.T) {
	encodeToFile := func(t *testing.T, filename string, closeErr error) error {
		resetRegistry(t)
		err := filetypes.RegisterEncoding(filetypes.DynamicEncoding{
			Name:       "closeerr",
			Extensions: []string{".closeerr"},
			Info:       filetypes.DynamicFileInfo{Form: "data"},
			Codec: filetypes.Codec{
				NewDecoder: newStubDecoder,
				NewEncoder: func(f *build.File, w io.Writer) (filetypes.Encoder, error) {
					return &captureEncoder{closeErr: closeErr}, nil
				},
			},
		})
		if err != nil {
			t.Fatalf("RegisterEncoding: %v", err)
		}
		ctx := cuecontext.New()
		enc, err := encoding.NewEncoder(ctx, &build.File{
			Filename: filename,
			Encoding: "closeerr",
			Form:     "data",
		}, &encoding.Config{Mode: filetypes.Export})
		if err != nil {
			t.Fatalf("NewEncoder: %v", err)
		}
		if err := enc.Encode(ctx.CompileString("a: 1")); err != nil {
			t.Fatalf("Encode: %v", err)
		}
		return enc.Close()
	}

	t.Run("codec failure aborts commit", func(t *testing.T) {
		codecErr := errors.New("codec close failed")
		filename := filepath.Join(t.TempDir(), "out.closeerr")
		err := encodeToFile(t, filename, codecErr)
		if !errors.Is(err, codecErr) {
			t.Fatalf("got error %v; want %v", err, codecErr)
		}
		if _, err := os.Stat(filename); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("committed output after codec close failed: %v", err)
		}
	})

	t.Run("successful codec close commits", func(t *testing.T) {
		filename := filepath.Join(t.TempDir(), "out.closeerr")
		if err := encodeToFile(t, filename, nil); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filename); err != nil {
			t.Fatalf("did not commit output after successful codec close: %v", err)
		}
	})

	t.Run("commit failure is returned", func(t *testing.T) {
		// Without Force, committing to an existing file fails.
		filename := filepath.Join(t.TempDir(), "out.closeerr")
		if err := os.WriteFile(filename, nil, 0o666); err != nil {
			t.Fatal(err)
		}
		err := encodeToFile(t, filename, nil)
		if !errors.Is(err, fs.ErrExist) {
			t.Fatalf("got error %v; want %v", err, fs.ErrExist)
		}
	})
}

func TestValidate(t *testing.T) {
	testCases := []struct {
		form build.Form
		in   string
		err  string
	}{{
		form: "data",
		in: `
		// Foo
		a: 2
		"b-b": 3
		s: -2
		a: +2
		`,
	}, {
		form: "graph",
		in: `
		let X = 3
		a: X
		"b-b": 3
		s: a
		`,
	},

		{form: "data", err: "imports", in: `import "foo" `},
		{form: "data", err: "references", in: `a: a`},
		{form: "data", err: "expressions", in: `a: 1 + 3`},
		{form: "data", err: "definitions", in: `#a: 1`},
		{form: "data", err: "constraints", in: `a: <1`},
		{form: "data", err: "expressions", in: `a: !true`},
		{form: "data", err: "expressions", in: `a: 1 | 2`},
		{form: "data", err: "expressions", in: `a: 1 | *2`},
		{form: "data", err: "references", in: `let X = 3, a: X`},
		{form: "data", err: "expressions", in: `2+2`},
		{form: "data", err: "expressions", in: `"\(3)"`},
		{form: "data", err: "expressions", in: `for x in [2] { a: 2 }`},
		{form: "data", err: "expressions", in: `a: len([])`},
		{form: "data", err: "ellipsis", in: `a: [...]`},
	}
	for _, tc := range testCases {
		t.Run(path.Join(string(tc.form), tc.in), func(t *testing.T) {
			f, err := parser.ParseFile("", tc.in, parser.ParseComments)
			if err != nil {
				t.Fatal(err)
			}
			d := encoding.NewDecoder(cuecontext.New(), &build.File{
				Filename: "foo.cue",
				Encoding: build.CUE,
				Form:     tc.form,
				Source:   f,
			}, nil)
			err = d.Err()
			if (tc.err == "") != (err == nil) {
				t.Errorf("got error %v; want error %v", err, tc.err != "")
			}
			if err != nil && !strings.Contains(err.Error(), tc.err) {
				t.Errorf("error message did not contain %q: %v", tc.err, err)
			}
		})
	}
}
