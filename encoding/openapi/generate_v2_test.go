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
	"encoding/json"
	"strings"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/encoding/jsonschema"
	"cuelang.org/go/encoding/openapi"
	"cuelang.org/go/internal/cuetxtar"
)

// generateV2Test runs a single #v2 txtar test through [openapi.GenerateV2]. It
// is dispatched from TestGenerateOpenAPI when a file carries the #v2 tag.
//
// Recognized tags:
//
//	#v2                        selects this runner
//	#Version: 3.0|3.1|crd      OpenAPI version (default 3.0)
//	#AllSchemas                set GenerateConfig.AllSchemas
//	#NamesFunc: <name>         a named func from v2NamesFuncs
//	#DescriptionFunc: <name>   a named func from v2DescriptionFuncs
//	#ExpectError: <substring>  expect generation to fail with this message
func generateV2Test(t *cuetxtar.Test) {
	ctx := t.CueContext()
	v := ctx.BuildInstance(t.Instance())
	if err := v.Err(); err != nil {
		t.Fatal(errors.Details(err, nil))
	}

	cfg := openapi.GenerateConfig{
		AllSchemas: t.HasTag("AllSchemas"),
	}
	if s, ok := t.Value("Version"); ok {
		cfg.Version = v2Version(t, s)
	}
	if name, ok := t.Value("NamesFunc"); ok {
		fn, found := v2NamesFuncs[name]
		if !found {
			t.Fatal("unknown NamesFunc", name)
		}
		cfg.NamesFunc = fn
	}
	if name, ok := t.Value("DescriptionFunc"); ok {
		fn, found := v2DescriptionFuncs[name]
		if !found {
			t.Fatal("unknown DescriptionFunc", name)
		}
		cfg.DescriptionFunc = fn
	}

	expectedErr, shouldErr := t.Value("ExpectError")
	e, err := openapi.GenerateV2(v, &cfg)
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

	doc := ctx.BuildExpr(e)
	qt.Assert(t, qt.IsNil(doc.Err()))

	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "   ")
	qt.Assert(t, qt.IsNil(enc.Encode(doc)))
	_, err = t.Writer("out.json").Write(out.Bytes())
	qt.Assert(t, qt.IsNil(err))
}

func v2Version(t *cuetxtar.Test, s string) openapi.Version {
	switch s {
	case "3.0":
		return openapi.Version3_0
	case "3.1":
		return openapi.Version3_1
	case "crd":
		return openapi.VersionKubernetesCRD
	}
	t.Fatalf("unknown #Version tag %q", s)
	return openapi.VersionUnknown
}

var v2NamesFuncs = map[string]func(refs []*jsonschema.CUERef){
	// toUpper names each shared schema by upper-casing the last element of its
	// path, joining a multi-element path with underscores.
	"toUpper": func(refs []*jsonschema.CUERef) {
		for _, r := range refs {
			var buf strings.Builder
			for i, sel := range r.Path.Selectors() {
				if i > 0 {
					buf.WriteByte('_')
				}
				buf.WriteString(strings.ToUpper(strings.TrimPrefix(sel.String(), "#")))
			}
			r.Name = buf.String()
		}
	},
}

var v2DescriptionFuncs = map[string]func(v cue.Value) string{
	"randomish": func(v cue.Value) string {
		return "Randomly picked description from a set of size one."
	},
}
