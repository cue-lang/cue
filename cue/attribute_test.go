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

package cue

import (
	"fmt"
	"testing"

	"cuelang.org/go/cue/errors"
)

func TestAttributes(t *testing.T) {
	const config = `
	a: {
		a: 0 @foo(a,b,c=1)
		b: 1 @bar(a,b,c,d=1) @foo(a,,d=1)
	}
	b: {
		@embed(foo)
		3
	} @field(foo)

	`

	testCases := []struct {
		flags AttrKind
		path  string
		out   string
	}{{
		flags: FieldAttr,
		path:  "a.a",
		out:   "[@foo(a,b,c=1)]",
	}, {
		flags: FieldAttr,
		path:  "a.b",
		out:   "[@bar(a,b,c,d=1) @foo(a,,d=1)]",
	}, {
		flags: DeclAttr,
		path:  "b",
		out:   "[@embed(foo)]",
	}, {
		flags: FieldAttr,
		path:  "b",
		out:   "[@field(foo)]",
	}, {
		flags: ValueAttr,
		path:  "b",
		out:   "[@field(foo) @embed(foo)]",
	}}
	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			v := getInstance(t, config).Value().LookupPath(ParsePath(tc.path))
			a := v.Attributes(tc.flags)
			got := fmt.Sprint(a)
			if got != tc.out {
				t.Errorf("got %v; want %v", got, tc.out)
			}

		})
	}
}

func TestAttributeErr(t *testing.T) {
	const config = `
	a: {
		a: 0 @foo(a,b,c=1)
		b: 1 @bar(a,b,c,d=1) @foo(a,,d=1)
	}
	`
	testCases := []struct {
		path string
		attr string
		err  error
	}{{
		path: "a",
		attr: "foo",
		err:  nil,
	}, {
		path: "a",
		attr: "bar",
		err:  errors.New(`attribute "bar" does not exist`),
	}, {
		path: "xx",
		attr: "bar",
		err:  errors.New(`attribute "bar" does not exist`),
	}, {
		path: "e",
		attr: "bar",
		err:  errors.New(`attribute "bar" does not exist`),
	}}
	for _, tc := range testCases {
		t.Run(tc.path+"-"+tc.attr, func(t *testing.T) {
			v := getInstance(t, config).Value().Lookup("a", tc.path)
			a := v.Attribute(tc.attr)
			err := a.Err()
			if !cmpError(err, tc.err) {
				t.Errorf("got %v; want %v", err, tc.err)
			}
		})
	}
}

func TestAttributeString(t *testing.T) {
	const config = `
	a: {
		a: 0 @foo(a,b,c=1)
		b: 1 @bar(a,b,c,d=1) @foo(a,,d=1)
	}
	`
	testCases := []struct {
		path string
		attr string
		pos  int
		str  string
		err  error
	}{{
		path: "a",
		attr: "foo",
		pos:  0,
		str:  "a",
	}, {
		path: "a",
		attr: "foo",
		pos:  2,
		str:  "c=1",
	}, {
		path: "b",
		attr: "bar",
		pos:  3,
		str:  "d=1",
	}, {
		path: "e",
		attr: "bar",
		err:  errors.New(`attribute "bar" does not exist`),
	}, {
		path: "b",
		attr: "foo",
		pos:  4,
		err:  errors.New("field does not exist"),
	}}
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s.%s:%d", tc.path, tc.attr, tc.pos), func(t *testing.T) {
			v := getInstance(t, config).Value().Lookup("a", tc.path)
			a := v.Attribute(tc.attr)
			got, err := a.String(tc.pos)
			if !cmpError(err, tc.err) {
				t.Errorf("err: got %v; want %v", err, tc.err)
			}
			if got != tc.str {
				t.Errorf("str: got %v; want %v", got, tc.str)
			}
		})
	}
}

func TestAttributeInt(t *testing.T) {
	const config = `
	a: {
		a: 0 @foo(1,3,c=1)
		b: 1 @bar(a,-4,c,d=1) @foo(a,,d=1)
	}
	`
	testCases := []struct {
		path string
		attr string
		pos  int
		val  int64
		err  error
	}{{
		path: "a",
		attr: "foo",
		pos:  0,
		val:  1,
	}, {
		path: "b",
		attr: "bar",
		pos:  1,
		val:  -4,
	}, {
		path: "e",
		attr: "bar",
		err:  errors.New(`attribute "bar" does not exist`),
	}, {
		path: "b",
		attr: "foo",
		pos:  4,
		err:  errors.New("field does not exist"),
	}, {
		path: "a",
		attr: "foo",
		pos:  2,
		err:  errors.New(`strconv.ParseInt: parsing "c=1": invalid syntax`),
	}}
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s.%s:%d", tc.path, tc.attr, tc.pos), func(t *testing.T) {
			v := getInstance(t, config).Value().Lookup("a", tc.path)
			a := v.Attribute(tc.attr)
			got, err := a.Int(tc.pos)
			if !cmpError(err, tc.err) {
				t.Errorf("err: got %v; want %v", err, tc.err)
			}
			if got != tc.val {
				t.Errorf("val: got %v; want %v", got, tc.val)
			}
		})
	}
}

func TestAttributeFlag(t *testing.T) {
	const config = `
	a: {
		a: 0 @foo(a,b,c=1)
		b: 1 @bar(a,b,c,d=1) @foo(a,,d=1)
	}
	`
	testCases := []struct {
		path string
		attr string
		pos  int
		flag string
		val  bool
		err  error
	}{{
		path: "a",
		attr: "foo",
		pos:  0,
		flag: "a",
		val:  true,
	}, {
		path: "b",
		attr: "bar",
		pos:  1,
		flag: "a",
		val:  false,
	}, {
		path: "b",
		attr: "bar",
		pos:  0,
		flag: "c",
		val:  true,
	}, {
		path: "e",
		attr: "bar",
		err:  errors.New(`attribute "bar" does not exist`),
	}, {
		path: "b",
		attr: "foo",
		pos:  4,
		err:  errors.New("field does not exist"),
	}}
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s.%s:%d", tc.path, tc.attr, tc.pos), func(t *testing.T) {
			v := getInstance(t, config).Value().Lookup("a", tc.path)
			a := v.Attribute(tc.attr)
			got, err := a.Flag(tc.pos, tc.flag)
			if !cmpError(err, tc.err) {
				t.Errorf("err: got %v; want %v", err, tc.err)
			}
			if got != tc.val {
				t.Errorf("val: got %v; want %v", got, tc.val)
			}
		})
	}
}

func TestAttributeLookup(t *testing.T) {
	const config = `
	a: {
		a: 0 @foo(a,b,c=1)
		b: 1 @bar(a,b,e =-5,d=1) @foo(a,,d=1)
	}
	`
	testCases := []struct {
		path string
		attr string
		pos  int
		key  string
		val  string
		err  error
	}{{
		path: "a",
		attr: "foo",
		pos:  0,
		key:  "c",
		val:  "1",
	}, {
		path: "b",
		attr: "bar",
		pos:  1,
		key:  "a",
		val:  "",
	}, {
		path: "b",
		attr: "bar",
		pos:  0,
		key:  "e",
		val:  "-5",
	}, {
		path: "b",
		attr: "bar",
		pos:  0,
		key:  "d",
		val:  "1",
	}, {
		path: "b",
		attr: "foo",
		pos:  2,
		key:  "d",
		val:  "1",
	}, {
		path: "b",
		attr: "foo",
		pos:  2,
		key:  "f",
		val:  "",
	}, {
		path: "e",
		attr: "bar",
		err:  errors.New(`attribute "bar" does not exist`),
	}, {
		path: "b",
		attr: "foo",
		pos:  4,
		err:  errors.New("field does not exist"),
	}}
	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%s.%s:%d", tc.path, tc.attr, tc.pos), func(t *testing.T) {
			v := getInstance(t, config).Value().Lookup("a", tc.path)
			a := v.Attribute(tc.attr)
			got, _, err := a.Lookup(tc.pos, tc.key)
			if !cmpError(err, tc.err) {
				t.Errorf("err: got %v; want %v", err, tc.err)
			}
			if got != tc.val {
				t.Errorf("val: got %v; want %v", got, tc.val)
			}
		})
	}
}
