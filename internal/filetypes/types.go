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

package filetypes

import (
	_ "embed"
	"sync"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

//go:embed types.cue
var typesCUE string

var typesValue cue.Value
var knownExtensions map[string]bool

var typesInit = sync.OnceFunc(func() {
	ctx := cuecontext.New()
	typesValue = ctx.CompileString(typesCUE, cue.Filename("types.cue"))
	if err := typesValue.Err(); err != nil {
		panic(err)
	}
	if err := typesValue.LookupPath(cue.MakePath(cue.Str("knownExtensions"))).Decode(&knownExtensions); err != nil {
		panic(err)
	}
})
