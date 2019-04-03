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

// Package cuego allows using CUE constraints in Go programs.
//
// CUE constraints can be used to validate Go types as well as fill out
// missing struct fields that are implied from the constraints and the values
// already defined by the struct value.
//
// CUE constraints can be added through field tags or by associating
// CUE code with a Go type. The field tags method follows the usual
// Go pattern:
//
//     type Sum struct {
//         A int `cue:"C-B" json:",omitempty"`
//         B int `cue:"C-A" json:",omitempty"`
//         C int `cue:"A+B" json:",omitempty"`
//     }
//
//     func main() {
//         fmt.Println(cuego.Validate(&Sum{A: 1, B: 5, C: 7}))
//     }
//
// AddConstraints allows annotating Go types with any CUE constraints.
//
//
// Validating Go Values
//
// To check whether a struct's values satisfy its constraints, call Validate:
//
//   if err := cuego.Validate(p); err != nil {
//      return err
//   }
//
// Validation assumes that all values are filled in correctly and will not
// infer values. To automatically infer values, use Complete.
//
//
// Completing Go Values
//
// Package cuego can also be used to infer undefined values from a set of
// CUE constraints, for instance to fill out fields in a struct. A value
// is considered undefined if it is a nil pointer type or if it is a zero
// value and there is a JSON field tag with the omitempty flag.
// A Complete will implicitly validate a struct.
//
package cuego // import "cuelang.org/go/cuego"

// The first goal of this packages is to get the semantics right. After that,
// there are a lot of performance gains to be made:
// - cache the type info extracted during value (as opposed to type) conversion
// - remove the usage of mutex for value conversions
// - avoid the JSON round trip for Decode, as used in Complete
// - generate native code for validating and updating
