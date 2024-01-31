// Copyright 2023 The CUE Authors
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

package load_test

import (
	"fmt"
	"path/filepath"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"
)

func Example() {
	// Load the package "example" relative to the directory testdata/testmod.
	// Akin to loading via: cd testdata/testmod && cue export ./example
	insts := load.Instances([]string{"./example"}, &load.Config{
		Dir: filepath.Join("testdata", "testmod"),
	})

	// testdata/testmod/example just has one file without any build tags,
	// so we get a single instance as a result.
	fmt.Println("Number of instances:", len(insts))
	inst := insts[0]
	if err := inst.Err; err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("Instance module:", inst.Module)
	fmt.Println("Instance import path:", inst.ImportPath)
	fmt.Println()

	// Inspect the syntax trees.
	fmt.Println("Source files:")
	for _, file := range inst.Files {
		fmt.Println(filepath.Base(file.Filename), "with", len(file.Decls), "declarations")
	}
	fmt.Println()

	// Build the instance into a value.
	// We can also use BuildInstances for many instances at once.
	ctx := cuecontext.New()
	val := ctx.BuildInstance(inst)
	if err := val.Err(); err != nil {
		fmt.Println(err)
		return
	}

	// Inspect the contents of the value, such as one string field.
	fieldStr, err := val.LookupPath(cue.MakePath(cue.Str("output"))).String()
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println("Field string:", fieldStr)

	// Output:
	// Number of instances: 1
	// Instance module: mod.test/test
	// Instance import path: mod.test/test/example
	//
	// Source files:
	// example.cue with 3 declarations
	//
	// Field string: Hello Joe
}
