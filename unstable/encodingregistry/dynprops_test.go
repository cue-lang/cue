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

package encodingregistry_test

import (
	"io"
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/internal/filetypes"
	"cuelang.org/go/unstable/encodingregistry"
	"github.com/go-quicktest/qt"
)

func nopDecoder(f *build.File, r io.Reader) (encodingregistry.Decoder, error) {
	return decoderFunc(func() (ast.Expr, error) { return nil, io.EOF }), nil
}

type decoderFunc func() (ast.Expr, error)

func (d decoderFunc) Decode() (ast.Expr, error) { return d() }

// TestDynamicPropertiesPreserved checks that a dynamic encoding's
// declared file-type properties are not silently dropped: an aspect
// override alongside an interpretation, a per-mode overlay on the
// explicit-qualifier path, and a declared form's default and
// refinement domain.
func TestDynamicPropertiesPreserved(t *testing.T) {
	// A declared aspect override must survive even when an
	// interpretation is also present, so the interpretation must not
	// cause the encoding's own form/aspect entry to be skipped.
	t.Run("aspects-with-interpretation", func(t *testing.T) {
		reset(t)
		err := encodingregistry.Register(encodingregistry.Encoding{
			Name:       "asp",
			Extensions: []string{".asp"},
			Info: encodingregistry.FileInfo{
				Interpretation: "jsonschema",
				Aspects:        map[encodingregistry.Aspect]bool{encodingregistry.AspectStream: true},
			},
			NewDecoder: nopDecoder,
		})
		qt.Assert(t, qt.IsNil(err))

		fi, err := filetypes.FromFile(&build.File{Encoding: "asp", Interpretation: "jsonschema"}, filetypes.Input)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.IsTrue(fi.Stream)) // declared aspect honored
	})

	// A per-mode overlay must apply on the explicit-qualifier path,
	// not only the extension path.
	t.Run("permode-on-qualifier", func(t *testing.T) {
		reset(t)
		err := encodingregistry.Register(encodingregistry.Encoding{
			Name:       "pm",
			Extensions: []string{".pm"},
			PerMode: map[encodingregistry.Mode]encodingregistry.FileInfo{
				encodingregistry.Input: {Interpretation: "jsonschema"},
			},
			NewDecoder: nopDecoder,
		})
		qt.Assert(t, qt.IsNil(err))

		// Extension path.
		fe, err := filetypes.ParseFile("x.pm", filetypes.Input)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(fe.Interpretation, build.Interpretation("jsonschema")))

		// Qualifier path, which must apply the overlay too.
		fq, err := filetypes.ParseFileAndType("x", "pm", filetypes.Input)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(fq.Interpretation, build.Interpretation("jsonschema")))
	})

	// A declared form supplies the built-in default and retains the
	// built-in refinement domain. Schema can be explicitly refined to
	// final, while dag defaults to dag and can refine to data but not back
	// to graph.
	t.Run("form-schema-refinement", func(t *testing.T) {
		reset(t)
		err := encodingregistry.Register(encodingregistry.Encoding{
			Name:       "sch",
			Extensions: []string{".sch"},
			Info:       encodingregistry.FileInfo{Form: "schema"},
			NewDecoder: nopDecoder,
		})
		qt.Assert(t, qt.IsNil(err))

		fi, err := filetypes.FromFile(&build.File{Encoding: "sch"}, filetypes.Input)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(fi.Form, build.Schema))

		fi, err = filetypes.FromFile(&build.File{Encoding: "sch", Form: build.Final}, filetypes.Input)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(fi.Form, build.Final))
	})
	t.Run("form-dag-refinement", func(t *testing.T) {
		reset(t)
		err := encodingregistry.Register(encodingregistry.Encoding{
			Name:       "dg",
			Extensions: []string{".dg"},
			Info:       encodingregistry.FileInfo{Form: "dag"},
			NewDecoder: nopDecoder,
		})
		qt.Assert(t, qt.IsNil(err))

		fi, err := filetypes.FromFile(&build.File{Encoding: "dg"}, filetypes.Input)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(fi.Form, build.DAG))

		fi, err = filetypes.FromFile(&build.File{Encoding: "dg", Form: build.Data}, filetypes.Input)
		qt.Assert(t, qt.IsNil(err))
		qt.Assert(t, qt.Equals(fi.Form, build.Data))

		// The dag form admits dag and data, but not graph. The
		// rejection names the form, the encoding, and the mode rather
		// than reporting a missing encoding.
		_, err = filetypes.FromFile(&build.File{Encoding: "dg", Form: build.Graph}, filetypes.Input)
		qt.Assert(t, qt.ErrorMatches(err, `form "graph" is not supported for encoding "dg" in input mode`))
	})
}
