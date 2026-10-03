// Copyright 2023 CUE Authors
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
	"fmt"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/pkg/internal/builtintest"
)

func TestBuiltin(t *testing.T) {
	builtintest.Run("openapi", t)
}

// TestMarshalSchemaError checks that an encoding/openapi error for a value
// which is not itself an error is reported as the result of the call.
func TestMarshalSchemaError(t *testing.T) {
	const src = `
import (
	"encoding/openapi"
	"strings"
)

k: int
config: openapi.#Config & {version: "3.0.0", info: {title: "T", version: "1"}}
schema: #S: s: strings.MinRunes(k)
result: openapi.MarshalSchema(config, schema)
`
	got := func() (s string) {
		defer func() {
			if r := recover(); r != nil {
				s = fmt.Sprintf("panic: %v", r)
			}
		}()
		v := cuecontext.New().CompileString(src, cue.Filename("in.cue"))
		return errors.Details(v.LookupPath(cue.ParsePath("result")).Err(), nil)
	}()
	// The call panics instead of reporting the openapi error.
	qt.Assert(t, qt.Equals(got, "panic: runtime error: invalid memory address or nil pointer dereference"))
}
