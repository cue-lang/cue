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

package reflect_test

import (
	"fmt"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/pkg/internal/builtintest"
)

func TestBuiltin(t *testing.T) {
	builtintest.Run("reflect", t)
}

// TestOpenedInScope asks about a struct opened with the postfix ... operator
// that was evaluated before the question is compiled, as a value in whose
// scope the question is compiled. The answers must not depend on which
// evaluation saw the operator.
func TestOpenedInScope(t *testing.T) {
	ctx := cuecontext.New()
	base := ctx.CompileString(`
		#N: {x: {a: 1}}
		shared:  #N...
		unified: #N... & {b: 1}
	`)
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	v := ctx.CompileString(`
		@experiment(predicates)

		import "reflect"

		sharedAllows:  reflect.Allows(shared, "zz")
		sharedExists:  exists(shared.zz)
		unifiedAllows: reflect.Allows(unified, "zz")
		unifiedExists: exists(unified.zz)
	`, cue.Scope(base))
	for _, tc := range []struct {
		path string
		want string
	}{
		{"sharedAllows", "true"},
		{"sharedExists", "false"},
		{"unifiedAllows", "true"},
		{"unifiedExists", "false"},
	} {
		x := v.LookupPath(cue.ParsePath(tc.path))
		got := fmt.Sprint(x)
		if err := x.Err(); err != nil {
			got = "error"
			t.Logf("%s: %v", tc.path, err)
		}
		if got != tc.want {
			t.Errorf("%s: got %s, want %s", tc.path, got, tc.want)
		}
	}
}
