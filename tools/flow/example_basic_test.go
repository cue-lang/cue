// Copyright 2023 CUE Authors
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

package flow_test

import (
	"context"
	"fmt"
	"log"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/tools/flow"
)

func Example() {
	ctx := cuecontext.New()
	v := ctx.CompileString(`
	a: {
		input: "world"
		output: string
	}
	b: {
		input: a.output
		output: string
	}
	`)
	if err := v.Err(); err != nil {
		log.Fatal(err)
	}
	controller := flow.New(nil, v, ioTaskFunc)
	if err := controller.Run(context.Background()); err != nil {
		log.Fatal(err)
	}
	// Output:
	// setting a.output to "hello world"
	// setting b.output to "hello hello world"
}

func ioTaskFunc(v cue.Value) (flow.Runner, error) {
	inputPath := cue.ParsePath("input")

	input := v.LookupPath(inputPath)
	if !input.Exists() {
		return nil, nil
	}

	return flow.RunnerFunc(func(t *flow.Task) error {
		inputVal, err := t.Value().LookupPath(inputPath).String()
		if err != nil {
			return fmt.Errorf("input not of type string")
		}

		outputVal := fmt.Sprintf("hello %s", inputVal)
		fmt.Printf("setting %s.output to %q\n", t.Path(), outputVal)

		return t.Fill(map[string]string{
			"output": outputVal,
		})
	}), nil
}
