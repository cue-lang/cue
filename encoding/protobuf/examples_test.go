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

package protobuf_test

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"cuelang.org/go/cue/format"
	"cuelang.org/go/encoding/protobuf"
)

func ExampleExtract() {
	cwd, _ := os.Getwd()
	var paths = []string{}
	paths = append(paths, cwd)
	paths = append(paths, filepath.Join(cwd, "testdata"))

	f, err := protobuf.Extract("examples/basic/basic.proto", nil, &protobuf.Config{
		Paths: paths,
	})

	if err != nil {
		log.Fatal(err, "")
	}

	b, _ := format.Node(f)
	fmt.Println(string(b))

	// Output:
	// // Package basic is just that: basic.
	// package basic
	//
	// // This is my type.
	// MyType: {
	// 	stringValue?: string @protobuf(1,name=string_value) // just any 'ole string
	//
	// 	// A method must start with a capital letter.
	// 	method?: [...string] @protobuf(2)
	// 	method?: [...=~"^[A-Z]"]
	// 	exmapleMap?: {
	// 		[string]: string
	// 	} @protobuf(3,type=map<string,string>,exmaple_map)
	// }
}
