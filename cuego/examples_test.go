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

package cuego_test

import (
	"fmt"
	"strings"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cuego"
)

func ExampleComplete_structTag() {
	type Sum struct {
		A int `cue:"C-B" json:",omitempty"`
		B int `cue:"C-A" json:",omitempty"`
		C int `cue:"A+B" json:",omitempty"`
	}

	a := Sum{A: 1, B: 5}
	err := cuego.Complete(&a)
	fmt.Printf("completed: %#v (err: %v)\n", a, err)

	a = Sum{A: 2, C: 8}
	err = cuego.Complete(&a)
	fmt.Printf("completed: %#v (err: %v)\n", a, err)

	a = Sum{A: 2, B: 3, C: 8}
	err = cuego.Complete(&a)
	fmt.Println(errMsg(err))

	//Output:
	// completed: cuego_test.Sum{A:1, B:5, C:6} (err: <nil>)
	// completed: cuego_test.Sum{A:2, B:6, C:8} (err: <nil>)
	// 2 errors in empty disjunction:
	// conflicting values null and {A:2,B:3,C:8} (mismatched types null and struct)
	// B: conflicting values 6 and 3
}

func ExampleConstrain() {
	type Config struct {
		Filename string
		OptFile  string `json:",omitempty"`
		MaxCount int
		MinCount int

		// TODO: show a field with time.Time
	}

	err := cuego.Constrain(&Config{}, `{
		let jsonFile = =~".json$"

		// Filename must be defined and have a .json extension
		Filename: jsonFile

		// OptFile must be undefined or be a file name with a .json extension
		OptFile?: jsonFile

		MinCount: >0 & <=MaxCount
		MaxCount: <=10_000
	}`)

	fmt.Println("error:", errMsg(err))

	fmt.Println("validate:", errMsg(cuego.Validate(&Config{
		Filename: "foo.json",
		MaxCount: 1200,
		MinCount: 39,
	})))

	fmt.Println("validate:", errMsg(cuego.Validate(&Config{
		Filename: "foo.json",
		MaxCount: 12,
		MinCount: 39,
	})))

	fmt.Println("validate:", errMsg(cuego.Validate(&Config{
		Filename: "foo.jso",
		MaxCount: 120,
		MinCount: 39,
	})))

	// TODO(errors): fix bound message (should be "does not match")

	//Output:
	// error: nil
	// validate: nil
	// validate: 2 errors in empty disjunction:
	// conflicting values null and {Filename:"foo.json",MaxCount:12,MinCount:39} (mismatched types null and struct)
	// MinCount: invalid value 39 (out of bound <=12)
	// validate: 2 errors in empty disjunction:
	// conflicting values null and {Filename:"foo.jso",MaxCount:120,MinCount:39} (mismatched types null and struct)
	// Filename: invalid value "foo.jso" (out of bound =~".json$")
}

func errMsg(err error) string {
	a := []string{}
	for _, err := range errors.Errors(err) {
		a = append(a, err.Error())
	}
	s := strings.Join(a, "\n")
	if s == "" {
		return "nil"
	}
	return s
}
