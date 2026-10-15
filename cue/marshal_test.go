// Copyright 2019 CUE Authors
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
	"strings"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue"
)

func TestMarshalling(t *testing.T) {
	testCases := []struct {
		filename string
		input    string
		pkg      string
	}{{
		filename: "foo.cue",
		pkg:      "foo",
		input: `package foo

		A: int
		B: string
		`,
	}, {
		filename: "bar.cue",
		pkg:      "bar",
		input: `package bar

		"Hello world!"
		`,
	}, {
		filename: "qux.cue",
		input: `
			"Hello world!"
		`,
	}, {
		filename: "baz.cue",
		pkg:      "baz",
		input: `package baz

		import "strings"

		a: strings.TrimSpace("  Hello world!  ")
		`}}
	for _, tc := range testCases {
		t.Run(tc.filename, func(t *testing.T) {
			r := &cue.Runtime{}
			inst, err := r.Compile(tc.filename, tc.input)
			if err != nil {
				t.Fatal(err)
			}
			inst.ImportPath = "test/pkg"
			want := fmt.Sprint(inst.Value())

			val := inst.Value()
			b, err := r.Marshal(&val)
			if err != nil {
				t.Fatal(err)
			}

			r2 := &cue.Runtime{}
			instances, err := r2.Unmarshal(b)
			if err != nil {
				t.Fatal(err)
			}
			inst = instances[0]

			if inst.ImportPath != "test/pkg" {
				t.Error("import path was not restored")
			}
			got := fmt.Sprint(inst.Value())

			if got != want {
				t.Errorf("\ngot:  %q;\nwant: %q", got, want)
			}
		})
	}
}

func TestMarshalMultiPackage(t *testing.T) {
	files := func(s ...string) []string { return s }
	insts := func(i ...*bimport) []*bimport { return i }
	pkg1 := &bimport{
		"mod.test/foo/pkg1",
		files(`
		package pkg1

		Object: "World"
		`),
	}
	pkg2 := &bimport{
		"mod.test/foo/pkg2",
		files(`
		package pkg

		Number: 12
		`),
	}

	testCases := []struct {
		instances []*bimport
		roots     []*bimport // root instances with an import path
		emit      string
	}{{
		insts(&bimport{"", files(`test: "ok"`)}),
		nil,
		`{test: "ok"}`,
	}, {
		insts(&bimport{"",
			files(
				`package test

		import math2 "math"

		"Pi: \(math2.Pi)!"`)}),
		nil,
		`"Pi: 3.14159265358979323846264338327950288419716939937510582097494459!"`,
	}, {
		insts(pkg1, &bimport{"",
			files(
				`package test

			import "mod.test/foo/pkg1"

			"Hello \(pkg1.Object)!"`),
		}),
		insts(pkg1),
		`"Hello World!"`,
	}, {
		insts(pkg1, &bimport{"",
			files(
				`package test

		import pkg2 "mod.test/foo/pkg1"
		"Hello \(pkg1)!"

		pkg1: pkg2.Object`),
		}),
		insts(pkg1),
		`"Hello World!"`,
	}, {
		insts(pkg2, &bimport{"",
			files(
				`package test

		import "mod.test/foo/pkg2"

		"Hello \(pkg.Number)!"`),
		}),
		insts(pkg2),
		`"Hello 12!"`,
	}, {
		// Test that a non-root dependency is correctly included
		// in the marshaled data.
		insts(pkg1, &bimport{"",
			files(
				`package test

		import "mod.test/foo/pkg1"

		"Hello \(pkg1.Object)!"`),
		}),
		nil,
		`"Hello World!"`,
	}}

	strValue := func(a []*cue.Instance) (ret []string) {
		for _, i := range a {
			ret = append(ret, strings.TrimSpace((fmt.Sprint(i.Value()))))
		}
		return ret
	}

	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			r := &cue.Runtime{}

			insts, err := r.BuildInstances(makeInstances(tc.instances, tc.roots...))
			if err != nil {
				t.Fatal(err)
			}
			want := strValue(insts)

			vals := make([]cue.InstanceOrValue, 0)
			for _, inst := range insts {
				val := inst.Value()
				vals = append(vals, &val)
			}

			b, err := r.Marshal(vals...)
			if err != nil {
				t.Fatal(err)
			}

			r2 := &cue.Runtime{}
			insts, err = r2.Unmarshal(b)
			if err != nil {
				t.Fatal(err)
			}
			got := strValue(insts)

			qt.Assert(t, qt.DeepEquals(got, want))
		})
	}
}
