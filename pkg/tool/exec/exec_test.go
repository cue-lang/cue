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

package exec

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"

	"cuelang.org/go/cue"
	"cuelang.org/go/internal/task"
)

func TestEnv(t *testing.T) {
	testCases := []struct {
		desc string
		val  string
		env  []string
	}{{
		desc: "mapped",
		val: `
		cmd: "echo"
		env: {
			WHO:  "World"
			WHAT: "Hello"
			WHEN: "Now!"
		}
		`,
		env: []string{"WHO=World", "WHAT=Hello", "WHEN=Now!"},
	}, {
		val: `
		cmd: "echo"
		env: [ "WHO=World", "WHAT=Hello", "WHEN=Now!" ]
		`,
		env: []string{"WHO=World", "WHAT=Hello", "WHEN=Now!"},
	}}
	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			var r cue.Runtime
			inst, err := r.Compile(tc.desc, tc.val)
			if err != nil {
				t.Fatal(err)
			}

			cmd, _, err := mkCommand(&task.Context{
				Context: context.Background(),
				Obj:     inst.Value(),
			})
			if err != nil {
				t.Fatal(err)
			}

			if !cmp.Equal(cmd.Env, tc.env) {
				t.Error(cmp.Diff(cmd.Env, tc.env))
			}
		})
	}
}
