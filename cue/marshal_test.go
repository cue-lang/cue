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

package cue

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
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
			r := &Runtime{}
			inst, err := r.Compile(tc.filename, tc.input)
			if err != nil {
				t.Fatal(err)
			}
			inst.ImportPath = "test/pkg"
			want := fmt.Sprint(inst.Value())

			b, err := r.Marshal(inst)
			if err != nil {
				t.Fatal(err)
			}

			r2 := &Runtime{}
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
	files := func(s ...string) (a []fileData) {
		for i, s := range s {
			a = append(a, fileData{fmt.Sprintf("file%d.cue", i), []byte(s)})
		}
		return a
	}
	insts := func(i ...*instanceData) []*instanceData { return i }
	pkg1 := &instanceData{
		true,
		"pkg1",
		files(`
		package pkg1

		Object: "World"
		`),
	}
	pkg2 := &instanceData{
		true,
		"example.com/foo/pkg2",
		files(`
		package pkg

		Number: 12
		`),
	}

	testCases := []struct {
		instances []*instanceData
		emit      string
	}{{
		insts(&instanceData{true, "", files(`test: "ok"`)}),
		`{test: "ok"}`,
	}, {
		insts(&instanceData{true, "",
			files(
				`package test

		import math2 "math"

		"Pi: \(math2.Pi)!"`)}),
		`"Pi: 3.14159265358979323846264338327950288419716939937510582097494459!"`,
	}, {
		insts(pkg1, &instanceData{true, "",
			files(
				`package test

			import "pkg1"

			"Hello \(pkg1.Object)!"`),
		}),
		`"Hello World!"`,
	}, {
		insts(pkg1, &instanceData{true, "",
			files(
				`package test

		import pkg2 "pkg1"
		pkg1: pkg2.Object

		"Hello \(pkg1)!"`),
		}),
		`"Hello World!"`,
	}, {
		insts(pkg2, &instanceData{true, "",
			files(
				`package test

		import "example.com/foo/pkg2"

		"Hello \(pkg.Number)!"`),
		}),
		`"Hello 12!"`,
	}}

	strValue := func(a []*Instance) (ret []string) {
		for _, i := range a {
			ret = append(ret, strings.TrimSpace((fmt.Sprint(i.Value()))))
		}
		return ret
	}

	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			r := &Runtime{}

			insts, err := compileInstances(r, tc.instances)
			if err != nil {
				t.Fatal(err)
			}
			want := strValue(insts)

			b, err := r.Marshal(insts...)
			if err != nil {
				t.Fatal(err)
			}

			r2 := &Runtime{}
			insts, err = r2.Unmarshal(b)
			if err != nil {
				t.Fatal(err)
			}
			got := strValue(insts)

			if !cmp.Equal(got, want) {
				t.Error(cmp.Diff(got, want))
			}
		})
	}
}
