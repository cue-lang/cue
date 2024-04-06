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

//go:generate go run cuelang.org/go/cmd/cue cmd gen

package filetypes

import (
	_ "embed"
	"encoding/json"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
)

//go:embed types.cue
var typesCUE string

var typesValue = func() cue.Value {
	ctx := cuecontext.New()
	val := ctx.CompileString(typesCUE, cue.Filename("types.cue"))
	if err := val.Err(); err != nil {
		panic(err)
	}
	return val
}()

//go:embed types_gen.json
var knownTypesJSON string

var knownTypes struct {
	KnownExtensions map[string]bool
	SimpleModeFiles map[string]struct {
		Default     build.File
		ByExtension map[string]build.File
	}
}

func init() {
	if err := json.Unmarshal([]byte(knownTypesJSON), &knownTypes); err != nil {
		panic(err)
	}
}
