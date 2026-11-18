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

package gopurge

import "testing"

func TestFuncBodies(t *testing.T) {
	const src = `package p

type T struct {
	F int
}

var A = [...]int{1, 2}

var M = map[string]int{"a": 1}

func F() int {
	return 1
}
`
	const want = `package p

type T struct {
	F int
}

var A = [...]int{1, 2}

var M = map[string]int{      }

func F() int {
         
}
`
	got := string(FuncBodies([]byte(src)))
	if got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
	if len(got) != len(src) {
		t.Errorf("the length changed from %d to %d, so positions moved", len(src), len(got))
	}
}
