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

package encoding_test

import (
	"io"
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/internal/encoding"
	"cuelang.org/go/internal/filetypes"
)

// The tests in this file register synthetic encodings in the
// process-global registry. The registry is add-only and freezes on
// first use, so each test resets it via the internal testing hook.
func resetRegistry(t *testing.T) {
	t.Helper()
	filetypes.ResetDynamicRegistryForTesting()
	t.Cleanup(filetypes.ResetDynamicRegistryForTesting)
}

// stubDecoder keeps test registrations valid: the registration API
// requires every codec to provide a decoder.
type stubDecoder struct{}

func (stubDecoder) Decode() (ast.Expr, error) { return nil, io.EOF }

func newStubDecoder(f *build.File, r io.Reader) (filetypes.Decoder, error) {
	return stubDecoder{}, nil
}

// zeroValueDecoder violates the ValueDecoder contract by returning the
// zero cue.Value with a nil error.
type zeroValueDecoder struct{ done bool }

func (d *zeroValueDecoder) Decode() (cue.Value, error) {
	if d.done {
		return cue.Value{}, io.EOF
	}
	d.done = true
	return cue.Value{}, nil
}

// TestValueDecoderNonExistentValue verifies that a value-plane decoder
// returning a non-existent value (a codec contract violation, distinct
// from a semantic bottom, which exists) is reported as a decoding error
// before File or DecodedValue hand anything out, rather than flowing
// through as a silently empty document.
func TestValueDecoderNonExistentValue(t *testing.T) {
	resetRegistry(t)
	err := filetypes.RegisterEncoding(filetypes.DynamicEncoding{
		Name:       "vpzero",
		Extensions: []string{".vpzero"},
		Info:       filetypes.DynamicFileInfo{Form: "schema"},
		Codec: filetypes.Codec{
			Value: &encoding.ValueCodec{
				NewValueDecoder: func(ctx *cue.Context, f *build.File, r io.Reader) (encoding.ValueDecoder, error) {
					return &zeroValueDecoder{}, nil
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("RegisterEncoding: %v", err)
	}

	ctx := cuecontext.New()
	dec := encoding.NewDecoder(ctx, &build.File{
		Filename: "in.vpzero",
		Encoding: "vpzero",
		Form:     "schema",
		Source:   []byte("ignored"),
	}, &encoding.Config{Mode: filetypes.Input})
	defer dec.Close()

	// The contract violation must be observable immediately: Done must
	// report true and Err must carry the error before any document is
	// handed out.
	if !dec.Done() {
		t.Error("Done() = false after decoding a non-existent value; want true")
	}
	err = dec.Err()
	if err == nil {
		t.Fatal("Err() = nil after decoding a non-existent value; want contract-violation error")
	}
	if got := err.Error(); !strings.Contains(got, "non-existent value") || !strings.Contains(got, "in.vpzero") {
		t.Errorf("Err() = %q; want it to mention a non-existent value and the filename", got)
	}
	if _, ok := dec.DecodedValue(); ok {
		t.Error("DecodedValue() reported a value for a non-existent document")
	}
	// Defense in depth (finding C4): even in this error state, File must
	// not return nil — callers that guarded with Done would otherwise
	// dereference nil before observing the error.
	if f := dec.File(); f == nil {
		t.Error("File() = nil; want a non-nil (empty) file with the error in Err()")
	}
}

// captureEncoder records the syntax node handed to a registered
// syntax-plane encoder, and returns closeErr from Close.
type captureEncoder struct {
	node     ast.Node
	closeErr error
}

func (e *captureEncoder) Encode(n ast.Node) error { e.node = n; return nil }
func (e *captureEncoder) Close() error            { return e.closeErr }

// TestDynamicSyntaxEncoderSchemaAspects verifies that a registered
// syntax-plane encoding derives its concreteness and syntax projection
// from the resolved file info like the built-in CUE encoder, instead of
// hardcoding a concrete projection: a Form:"schema" registration must
// accept a non-concrete value in def mode and receive optional fields,
// definitions, and doc comments.
func TestDynamicSyntaxEncoderSchemaAspects(t *testing.T) {
	resetRegistry(t)
	capture := &captureEncoder{}
	err := filetypes.RegisterEncoding(filetypes.DynamicEncoding{
		Name:       "synschema",
		Extensions: []string{".synschema"},
		Info:       filetypes.DynamicFileInfo{Form: "schema"},
		Codec: filetypes.Codec{
			NewDecoder: newStubDecoder,
			NewEncoder: func(f *build.File, w io.Writer) (filetypes.Encoder, error) {
				return capture, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("RegisterEncoding: %v", err)
	}

	ctx := cuecontext.New()
	v := ctx.CompileString(`
// Doc comment for a.
a: int
b?: string
#D: int
`)
	if err := v.Err(); err != nil {
		t.Fatalf("CompileString: %v", err)
	}

	enc, err := encoding.NewEncoder(ctx, &build.File{
		Filename: "out.synschema",
		Encoding: "synschema",
		Form:     "schema",
	}, &encoding.Config{Mode: filetypes.Def, Out: io.Discard})
	if err != nil {
		t.Fatalf("NewEncoder: %v", err)
	}
	if enc.IsConcrete() {
		t.Error("IsConcrete() = true for a schema-form encoding in def mode; want false")
	}
	if err := enc.Encode(v); err != nil {
		t.Fatalf("Encode of a non-concrete schema value failed: %v", err)
	}
	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if capture.node == nil {
		t.Fatal("registered encoder did not receive a syntax node")
	}
	b, err := format.Node(capture.node)
	if err != nil {
		t.Fatalf("formatting captured node: %v", err)
	}
	got := string(b)
	for _, want := range []string{"b?:", "#D:", "Doc comment for a."} {
		if !strings.Contains(got, want) {
			t.Errorf("captured syntax is missing %q:\n%s", want, got)
		}
	}
}

// TestDynamicDecoderMissingConstructor exercises the dispatch guard for
// a codec without any decoder. Registration normally requires a decoder;
// if the registration API rejects the codec outright that rejection is
// the guard, and otherwise dispatch must report an error rather than
// call a nil constructor.
func TestDynamicDecoderMissingConstructor(t *testing.T) {
	resetRegistry(t)
	err := filetypes.RegisterEncoding(filetypes.DynamicEncoding{
		Name:       "enconly",
		Extensions: []string{".enconly"},
		Info:       filetypes.DynamicFileInfo{Form: "data"},
		Codec: filetypes.Codec{
			NewEncoder: func(f *build.File, w io.Writer) (filetypes.Encoder, error) {
				return &captureEncoder{}, nil
			},
		},
	})
	if err != nil {
		// A registration-time decoder requirement also prevents the nil
		// constructor from ever being reached; accept it as the guard.
		if !strings.Contains(strings.ToLower(err.Error()), "decoder") {
			t.Fatalf("RegisterEncoding failed for an unrelated reason: %v", err)
		}
		return
	}

	ctx := cuecontext.New()
	dec := encoding.NewDecoder(ctx, &build.File{
		Filename: "in.enconly",
		Encoding: "enconly",
		Form:     "data",
		Source:   []byte("x"),
	}, &encoding.Config{Mode: filetypes.Input}) // must not panic
	defer dec.Close()
	err = dec.Err()
	if err == nil || !strings.Contains(err.Error(), `unsupported encoding "enconly"`) {
		t.Fatalf("Err() = %v; want unsupported-encoding error", err)
	}
}
