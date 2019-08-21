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

	"cuelang.org/go/cue"
)

func ExampleRuntime_Parse() {
	const config = `
	msg:   "Hello \(place)!"
	place: string | *"world"
	`

	var r cue.Runtime

	instance, err := r.Compile("test", config)
	if err != nil {
		// handle error
	}

	str, _ := instance.Lookup("msg").String()
	fmt.Println(str)

	instance, _ = instance.Fill("you", "place")
	str, _ = instance.Lookup("msg").String()
	fmt.Println(str)

	// Output:
	// Hello world!
	// Hello you!
}

func ExampleValue_Decode() {
	type ab struct{ A, B int }

	var x ab

	var r cue.Runtime

	i, _ := r.Compile("test", `{A: 2, B: 4}`)
	_ = i.Value().Decode(&x)
	fmt.Println(x)

	i, _ = r.Compile("test", `{B: "foo"}`)
	err := i.Value().Decode(&x)
	fmt.Println(err)

	// Output:
	// {2 4}
	// json: cannot unmarshal string into Go struct field ab.B of type int
}
