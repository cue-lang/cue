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

package path_test

import (
	"fmt"

	"cuelang.org/go/pkg/path"
)

func ExampleSplitList() {
	fmt.Println("On Unix:", path.SplitList("/a/b/c:/usr/bin", path.Unix))
	// Output:
	// On Unix: [/a/b/c /usr/bin]
}

func ExampleRel() {
	paths := []string{
		"/a/b/c",
		"/b/c",
		"./b/c",
	}
	base := "/a"

	fmt.Println("On Unix:")
	for _, p := range paths {
		rel, err := path.Rel(base, p, path.Unix)
		fmt.Printf("%q: %q %v\n", p, rel, err)
	}

	// Output:
	// On Unix:
	// "/a/b/c": "b/c" <nil>
	// "/b/c": "../b/c" <nil>
	// "./b/c": "" Rel: can't make ./b/c relative to /a
}

func ExampleSplit() {
	paths := []string{
		"/home/arnie/amelia.jpg",
		"/mnt/photos/",
		"rabbit.jpg",
		"/usr/local//go",
	}
	fmt.Println("On Unix:")
	for _, p := range paths {
		pair := path.Split(p, path.Unix)
		fmt.Printf("input: %q\n\tdir: %q\n\tfile: %q\n", p, pair[0], pair[1])
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

func ExampleJoin() {
	fmt.Println("On Unix:")
	fmt.Println(path.Join([]string{"a", "b", "c"}, path.Unix))
	fmt.Println(path.Join([]string{"a", "b/c"}, path.Unix))
	fmt.Println(path.Join([]string{"a/b", "c"}, path.Unix))
	fmt.Println(path.Join([]string{"a/b", "/c"}, path.Unix))

	fmt.Println(path.Join([]string{"a/b", "../../../xyz"}, path.Unix))

	// Output:
	// On Unix:
	// a/b/c
	// a/b/c
	// a/b/c
	// a/b/c
	// ../xyz
}

func ExampleMatch() {
	fmt.Println("On Unix:")
	fmt.Println(path.Match("/home/catch/*", "/home/catch/foo", path.Unix))
	fmt.Println(path.Match("/home/catch/*", "/home/catch/foo/bar", path.Unix))
	fmt.Println(path.Match("/home/?opher", "/home/gopher", path.Unix))
	fmt.Println(path.Match("/home/\\*", "/home/*", path.Unix))

	// Output:
	// On Unix:
	// true <nil>
	// false <nil>
	// true <nil>
	// true <nil>
}

func ExampleBase() {
	fmt.Println("On Unix:")
	fmt.Println(path.Base("/foo/bar/baz.js", path.Unix))
	fmt.Println(path.Base("/foo/bar/baz", path.Unix))
	fmt.Println(path.Base("/foo/bar/baz/", path.Unix))
	fmt.Println(path.Base("dev.txt", path.Unix))
	fmt.Println(path.Base("../todo.txt", path.Unix))
	fmt.Println(path.Base("..", path.Unix))
	fmt.Println(path.Base(".", path.Unix))
	fmt.Println(path.Base("/", path.Unix))
	fmt.Println(path.Base("", path.Unix))

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

func ExampleDir() {
	fmt.Println("On Unix:")
	fmt.Println(path.Dir("/foo/bar/baz.js", path.Unix))
	fmt.Println(path.Dir("/foo/bar/baz", path.Unix))
	fmt.Println(path.Dir("/foo/bar/baz/", path.Unix))
	fmt.Println(path.Dir("/dirty//path///", path.Unix))
	fmt.Println(path.Dir("dev.txt", path.Unix))
	fmt.Println(path.Dir("../todo.txt", path.Unix))
	fmt.Println(path.Dir("..", path.Unix))
	fmt.Println(path.Dir(".", path.Unix))
	fmt.Println(path.Dir("/", path.Unix))
	fmt.Println(path.Dir("", path.Unix))

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

func ExampleIsAbs() {
	fmt.Println("On Unix:")
	fmt.Println(path.IsAbs("/home/gopher", path.Unix))
	fmt.Println(path.IsAbs(".bashrc", path.Unix))
	fmt.Println(path.IsAbs("..", path.Unix))
	fmt.Println(path.IsAbs(".", path.Unix))
	fmt.Println(path.IsAbs("/", path.Unix))
	fmt.Println(path.IsAbs("", path.Unix))

	// Output:
	// On Unix:
	// true
	// false
	// false
	// false
	// true
	// false
}
