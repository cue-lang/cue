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

// Copyright 2013 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package ospath_test

import (
	"fmt"

	"cuelang.org/go/internal/ospath"
)

func ExampleOS_SplitList() {
	fmt.Println("On Unix:", ospath.Unix.SplitList("/a/b/c:/usr/bin"))
	// Output:
	// On Unix: [/a/b/c /usr/bin]
}

func ExampleOS_Rel() {
	paths := []string{
		"/a/b/c",
		"/b/c",
		"./b/c",
	}
	base := "/a"

	fmt.Println("On Unix:")
	for _, p := range paths {
		rel, err := ospath.Unix.Rel(base, p)
		fmt.Printf("%q: %q %v\n", p, rel, err)
	}

	// Output:
	// On Unix:
	// "/a/b/c": "b/c" <nil>
	// "/b/c": "../b/c" <nil>
	// "./b/c": "" Rel: can't make ./b/c relative to /a
}

func ExampleOS_Split() {
	paths := []string{
		"/home/arnie/amelia.jpg",
		"/mnt/photos/",
		"rabbit.jpg",
		"/usr/local//go",
	}
	fmt.Println("On Unix:")
	for _, p := range paths {
		d, f := ospath.Unix.Split(p)
		fmt.Printf("input: %q\n\tdir: %q\n\tfile: %q\n", p, d, f)
	}
	// Output:
	// On Unix:
	// input: "/home/arnie/amelia.jpg"
	// 	dir: "/home/arnie/"
	// 	file: "amelia.jpg"
	// input: "/mnt/photos/"
	// 	dir: "/mnt/photos/"
	// 	file: ""
	// input: "rabbit.jpg"
	// 	dir: ""
	// 	file: "rabbit.jpg"
	// input: "/usr/local//go"
	// 	dir: "/usr/local//"
	// 	file: "go"
}

func ExampleOS_Join() {
	fmt.Println("On Unix:")
	fmt.Println(ospath.Unix.Join("a", "b", "c"))
	fmt.Println(ospath.Unix.Join("a", "b/c"))
	fmt.Println(ospath.Unix.Join("a/b", "c"))
	fmt.Println(ospath.Unix.Join("a/b", "/c"))

	fmt.Println(ospath.Unix.Join("a/b", "../../../xyz"))

	// Output:
	// On Unix:
	// a/b/c
	// a/b/c
	// a/b/c
	// a/b/c
	// ../xyz
}

func ExampleOS_Match() {
	fmt.Println("On Unix:")
	fmt.Println(ospath.Unix.Match("/home/catch/*", "/home/catch/foo"))
	fmt.Println(ospath.Unix.Match("/home/catch/*", "/home/catch/foo/bar"))
	fmt.Println(ospath.Unix.Match("/home/?opher", "/home/gopher"))
	fmt.Println(ospath.Unix.Match("/home/\\*", "/home/*"))

	// Output:
	// On Unix:
	// true <nil>
	// false <nil>
	// true <nil>
	// true <nil>
}

func ExampleOS_Base() {
	fmt.Println("On Unix:")
	fmt.Println(ospath.Unix.Base("/foo/bar/baz.js"))
	fmt.Println(ospath.Unix.Base("/foo/bar/baz"))
	fmt.Println(ospath.Unix.Base("/foo/bar/baz/"))
	fmt.Println(ospath.Unix.Base("dev.txt"))
	fmt.Println(ospath.Unix.Base("../todo.txt"))
	fmt.Println(ospath.Unix.Base(".."))
	fmt.Println(ospath.Unix.Base("."))
	fmt.Println(ospath.Unix.Base("/"))
	fmt.Println(ospath.Unix.Base(""))

	// Output:
	// On Unix:
	// baz.js
	// baz
	// baz
	// dev.txt
	// todo.txt
	// ..
	// .
	// /
	// .
}

func ExampleOS_Dir() {
	fmt.Println("On Unix:")
	fmt.Println(ospath.Unix.Dir("/foo/bar/baz.js"))
	fmt.Println(ospath.Unix.Dir("/foo/bar/baz"))
	fmt.Println(ospath.Unix.Dir("/foo/bar/baz/"))
	fmt.Println(ospath.Unix.Dir("/dirty//path///"))
	fmt.Println(ospath.Unix.Dir("dev.txt"))
	fmt.Println(ospath.Unix.Dir("../todo.txt"))
	fmt.Println(ospath.Unix.Dir(".."))
	fmt.Println(ospath.Unix.Dir("."))
	fmt.Println(ospath.Unix.Dir("/"))
	fmt.Println(ospath.Unix.Dir(""))

	// Output:
	// On Unix:
	// /foo/bar
	// /foo/bar
	// /foo/bar/baz
	// /dirty/path
	// .
	// ..
	// .
	// .
	// /
	// .
}

func ExampleOS_IsAbs() {
	fmt.Println("On Unix:")
	fmt.Println(ospath.Unix.IsAbs("/home/gopher"))
	fmt.Println(ospath.Unix.IsAbs(".bashrc"))
	fmt.Println(ospath.Unix.IsAbs(".."))
	fmt.Println(ospath.Unix.IsAbs("."))
	fmt.Println(ospath.Unix.IsAbs("/"))
	fmt.Println(ospath.Unix.IsAbs(""))

}
