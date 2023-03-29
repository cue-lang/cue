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

package tdtest_test

import (
	"testing"

	"cuelang.org/go/internal/tdtest"
)

// TODO: write a proper test

// NOTE: for debugging purposes. Do not remove.
func TestX(t *testing.T) {
	t.Skip()

	type testCase struct {
		name string
		want string
	}
	_, cases := 1, []testCase{{
		name: "foo",
		want: `foo`,
	}}

	td := tdtest.New(t, cases).Update(true)
	td.Run(func(t *tdtest.T, tc *testCase) {
		t.Equal(tc.want, "actual")
	})
}
