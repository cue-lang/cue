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

package openapi_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/encoding/openapi"
	"cuelang.org/go/internal/cuetxtar"
)

// TestExtractV2 runs the #extractv2 txtar tests through [openapi.ExtractV2].
func TestExtractV2(t *testing.T) {
	t.Parallel()
	test := cuetxtar.TxTarTest{
		Root:   "./testdata",
		Name:   t.Name(),
		Matrix: matrix,
	}
	test.Run(t, func(t *cuetxtar.Test) {
		t.Parallel()
		if !t.HasTag("extractv2") {
			return
		}
		extractV2Test(t)
	})
}

// extractV2Test runs a single #extractv2 txtar test. The input CUE value is
// treated as an OpenAPI document (JSON is a subset of CUE, so the document may
// be written directly as CUE data).
//
// Recognized tags:
//
//	#extractv2                 selects this runner
//	#Version: 3.0|3.1|crd      OpenAPI version (default: auto-detected)
//	#ExpectError: <substring>  expect extraction to fail with this message
func extractV2Test(t *cuetxtar.Test) {
	ctx := t.CueContext()
	v := ctx.BuildInstance(t.Instance())
	if err := v.Err(); err != nil {
		t.Fatal(errors.Details(err, nil))
	}

	var cfg openapi.ExtractConfig
	if s, ok := t.Value("Version"); ok {
		cfg.Version = v2Version(t, s)
	}

	expectedErr, shouldErr := t.Value("ExpectError")
	f, err := openapi.ExtractV2(v, &cfg)
	if err != nil {
		details := errors.Details(err, nil)
		if !shouldErr || !strings.Contains(details, expectedErr) {
			t.Fatal("unexpected error:", details)
		}
		return
	}
	if shouldErr {
		t.Fatal("unexpected success")
	}

	// The extracted CUE must compile.
	built := ctx.BuildFile(f)
	qt.Assert(t, qt.IsNil(built.Err()))

	// Round-trip: the extracted representation must be a valid GenerateV2 input.
	_, err = openapi.GenerateV2(built, &openapi.GenerateConfig{Version: cfg.Version})
	qt.Assert(t, qt.IsNil(err))

	b, err := format.Node(f, format.Simplify())
	qt.Assert(t, qt.IsNil(err))
	_, err = t.Writer("out.cue").Write(append(bytes.TrimSpace(b), '\n'))
	qt.Assert(t, qt.IsNil(err))
}
