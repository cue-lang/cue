// Copyright 2024 The CUE Authors
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

package errors_test

import (
	"fmt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
)

func Example() {
	v := cuecontext.New().CompileString(`
		a: string & 123
		b: int & "foo"
	`, cue.Filename("input.cue"))
	err := v.Validate()

	// The Error method only shows the first error encountered.
	fmt.Printf("string via the Error method:\n  %q\n\n", err)

	// [errors.Errors] allows listing all the errors encountered.
	fmt.Printf("list via errors.Errors:\n")
	for _, e := range errors.Errors(err) {
		fmt.Printf("  * %s\n", e)
	}
	fmt.Printf("\n")

	// [errors.Positions] lists the positions of all errors encountered.
	fmt.Printf("positions via errors.Positions:\n")
	for _, pos := range errors.Positions(err) {
		fmt.Printf("  * %s\n", pos)
	}
	fmt.Printf("\n")

	// [errors.Details] renders a human-friendly description of all errors like cmd/cue does.
	fmt.Printf("human-friendly string via errors.Details:\n")
	fmt.Println(errors.Details(err, nil))

	// Output:
	// string via the Error method:
	//   "a: conflicting values string and 123 (mismatched types string and int) (and 1 more errors)"
	//
	// list via errors.Errors:
	//   * a: conflicting values string and 123 (mismatched types string and int)
	//   * b: conflicting values int and "foo" (mismatched types int and string)
	//
	// positions via errors.Positions:
	//   * input.cue:2:6
	//   * input.cue:2:15
	//
	// human-friendly string via errors.Details:
	// a: conflicting values string and 123 (mismatched types string and int):
	//     input.cue:2:6
	//     input.cue:2:15
	// b: conflicting values int and "foo" (mismatched types int and string):
	//     input.cue:3:6
	//     input.cue:3:12
}
