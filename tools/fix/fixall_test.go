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

package fix

import (
	"fmt"
	"testing"

	"cuelang.org/go/cue/format"
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/cuetxtar"
)

func TestInstances(t *testing.T) {
	t.Skip()

	test := cuetxtar.TxTarTest{
		Root:   "./testdata",
		Name:   "fixmod",
		Update: cuetest.UpdateGoldenFiles,
	}

	test.Run(t, func(t *cuetxtar.Test) {
		a := t.ValidInstances("./...")
		err := Instances(a)
		t.WriteErrors(err)
		for _, b := range a {
			for _, f := range b.Files {
				b, _ := format.Node(f)
				fmt.Fprintln(t, "---", t.Rel(f.Filename))
				fmt.Fprintln(t, string(b))
			}
		}
	})
}
