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

package export

import (
	"flag"
	"testing"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/cuetxtar"
	"github.com/rogpeppe/go-internal/txtar"
)

var update = flag.Bool("update", false, "update the test files")

func TestDefinition(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root:   "./testdata",
		Name:   "definition",
		Update: *update,
	}

	r := runtime.New()

	test.Run(t, func(t *cuetxtar.Test) {
		a := t.ValidInstances()

		v, errs := compile.Files(nil, r, a[0].Files...)
		if errs != nil {
			t.Fatal(errs)
		}
		v.Finalize(eval.NewContext(r, v))

		// TODO: do we need to evaluate v? In principle not necessary.
		// v.Finalize(eval.NewContext(r, v))

		file, errs := Def(r, v)
		errors.Print(t, errs, nil)
		_, _ = t.Write(formatNode(t.T, file))
	})
}

func formatNode(t *testing.T, n ast.Node) []byte {
	t.Helper()

	b, err := format.Node(n)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// For debugging purposes. Do not delete.
func TestX(t *testing.T) {
	t.Skip()

	in := `
-- in.cue --
package test

// // Foo
// a: [X=string]: [Y=string]: {
// 	name: X+Y
// }

// [Y=string]: [X=string]: name: {Y+X}
// {
// 	name:  X.other + Y
// 	other: string
// }

// c: [X=string]: X

// #pkg1: Object

// "Hello \(#pkg1)!"


// Object: "World"

// // A Foo fooses stuff.
// foos are instances of Foo.
// foos: [string]: {}

// // // My first little foo.
// foos: MyFoo: {}

bar: 3
d2: C="foo\(bar)": {
    name: "xx"
    foo: C.name
}

	`

	archive := txtar.Parse([]byte(in))
	a := cuetxtar.Load(archive, "/tmp/test")
	if err := a[0].Err; err != nil {
		t.Fatal(err)
	}

	// x := a[0].Files[0]
	// astutil.Sanitize(x)

	r := runtime.New()
	v, errs := compile.Files(nil, r, a[0].Files...)
	if errs != nil {
		t.Fatal(errs)
	}
	v.Finalize(eval.NewContext(r, v))

	file, errs := Def(r, v)
	if errs != nil {
		t.Fatal(errs)
	}

	t.Error(string(formatNode(t, file)))
}
