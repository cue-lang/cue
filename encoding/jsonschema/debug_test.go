// Copyright 2025 CUE Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package jsonschema_test

import (
	"path"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/encoding/json"
	"cuelang.org/go/encoding/jsonschema"
	"cuelang.org/go/encoding/yaml"
	"cuelang.org/go/internal/astinternal"
	"golang.org/x/tools/txtar"
)

func TestX(t *testing.T) {
	t.Skip()
	data := `
-- schema.json --
`

	a := txtar.Parse([]byte(data))

	ctx := cuecontext.New()
	var v cue.Value
	var err error
	for _, f := range a.Files {
		switch path.Ext(f.Name) {
		case ".json":
			expr, err := json.Extract(f.Name, f.Data)
			if err != nil {
				t.Fatal(err)
			}
			v = ctx.BuildExpr(expr)
		case ".yaml":
			file, err := yaml.Extract(f.Name, f.Data)
			if err != nil {
				t.Fatal(err)
			}
			v = ctx.BuildFile(file)
		}
	}

	cfg := &jsonschema.Config{ID: "test"}
	expr, err := jsonschema.Extract(v, cfg)
	if err != nil {
		t.Fatal(err)
	}

	t.Fatal(astinternal.DebugStr(expr))
}
