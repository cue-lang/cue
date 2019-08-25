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
	"log"

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

func ExampleSubsumes() {
	// Check compatibility of successive APIs.
	var r cue.Runtime

	inst, err := r.Compile("apis", `
	// Release notes:
	// - You can now specify your age and your hobby!
	V1 :: {
		age:   >=0 & <=100
		hobby: string
	}

	// Release notes:
	// - People get to be older than 100, so we relaxed it.
	// - It seems not many people have a hobby, so we made it optional.
	V2 :: {
		age:    >=0 & <=150 // people get older now
		hobby?: string      // some people don't have a hobby
	}

	// Release notes:
	// - Actually no one seems to have a hobby nowadays anymore,
	//   so we dropped the field.
	V3 :: {
		age: >=0 & <=150
	}`)

	if err != nil {
		fmt.Println(err)
		// handle error
	}
	v1, err1 := inst.LookupField("V1")
	v2, err2 := inst.LookupField("V2")
	v3, err3 := inst.LookupField("V3")
	if err1 != nil || err2 != nil || err3 != nil {
		log.Println(err1, err2, err3)
	}

	// Is V2 backwards compatible with V1? In other words, does V2 subsume V1?
	pass := v2.Value.Subsumes(v1.Value)
	fmt.Println("V2 is backwards compatible with V1:", pass)

	pass = v3.Value.Subsumes(v2.Value)
	fmt.Println("V3 is backwards compatible with V2:", pass)

	// Output:
	// V2 is backwards compatible with V1: true
	// V3 is backwards compatible with V2: false
}
