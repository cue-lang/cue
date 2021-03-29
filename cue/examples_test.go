// Copyright 2021 CUE Authors
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

package cue_test

import (
	"fmt"
	"io/ioutil"
	"os"

	"cuelang.org/go/cue"
	"cuelang.org/go/internal/cuetxtar"
	"github.com/rogpeppe/go-internal/txtar"
)

func load(file string) *cue.Instance {
	dir, _ := ioutil.TempDir("", "*")
	defer os.RemoveAll(dir)

	inst := cue.Build(cuetxtar.Load(txtar.Parse([]byte(file)), dir))[0]
	if err := inst.Err; err != nil {
		panic(err)
	}
	return inst
}

func ExampleHid() {
	const file = `
-- cue.mod/module.cue --
module: "acme.com"

-- main.cue --
import "acme.com/foo:bar"

bar
_foo: int // scoped in main (anonymous) package
baz: _foo

-- foo/bar.cue --
package bar

_foo: int // scoped within imported package
bar: _foo
`

	v := load(file).Value()

	v = v.FillPath(cue.MakePath(cue.Hid("_foo", "acme.com/foo:bar")), 1)
	v = v.FillPath(cue.MakePath(cue.Hid("_foo", "_")), 2)
	fmt.Println(v.LookupPath(cue.ParsePath("bar")).Int64())
	fmt.Println(v.LookupPath(cue.ParsePath("baz")).Int64())

	// Output:
	// 1 <nil>
	// 2 <nil>
}
