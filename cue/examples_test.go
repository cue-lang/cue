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
	"cuelang.org/go/cue/cuecontext"
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

func ExampleValue_Allows() {
	ctx := cuecontext.New()

	const file = `
a: [1, 2, ...int]

b: #Point
#Point: {
	x:  int
	y:  int
	z?: int
}

c: [string]: int

d: #C
#C: [>"m"]: int
`

	v := ctx.CompileString(file)

	a := v.LookupPath(cue.ParsePath("a"))
	fmt.Println("a allows:")
	fmt.Println("  index 4:       ", a.Allows(cue.Index(4)))
	fmt.Println("  any index:     ", a.Allows(cue.AnyIndex))
	fmt.Println("  any string:    ", a.Allows(cue.AnyString))

	b := v.LookupPath(cue.ParsePath("b"))
	fmt.Println("b allows:")
	fmt.Println("  field x:       ", b.Allows(cue.Str("x")))
	fmt.Println("  field z:       ", b.Allows(cue.Str("z")))
	fmt.Println("  field foo:     ", b.Allows(cue.Str("foo")))
	fmt.Println("  index 4:       ", b.Allows(cue.Index(4)))
	fmt.Println("  any string:    ", b.Allows(cue.AnyString))

	c := v.LookupPath(cue.ParsePath("c"))
	fmt.Println("c allows:")
	fmt.Println("  field z:       ", c.Allows(cue.Str("z")))
	fmt.Println("  field foo:     ", c.Allows(cue.Str("foo")))
	fmt.Println("  index 4:       ", c.Allows(cue.Index(4)))
	fmt.Println("  any string:    ", c.Allows(cue.AnyString))

	d := v.LookupPath(cue.ParsePath("d"))
	fmt.Println("d allows:")
	fmt.Println("  field z:       ", d.Allows(cue.Str("z")))
	fmt.Println("  field foo:     ", d.Allows(cue.Str("foo")))
	fmt.Println("  index 4:       ", d.Allows(cue.Index(4)))
	fmt.Println("  any string:    ", d.Allows(cue.AnyString))

	// Output:
	// a allows:
	//   index 4:        true
	//   any index:      true
	//   any string:     false
	// b allows:
	//   field x:        true
	//   field z:        true
	//   field foo:      false
	//   index 4:        false
	//   any string:     false
	// c allows:
	//   field z:        true
	//   field foo:      true
	//   index 4:        false
	//   any string:     true
	// d allows:
	//   field z:        true
	//   field foo:      false
	//   index 4:        false
	//   any string:     false
}
