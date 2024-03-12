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
	"os"
	"path/filepath"

	"golang.org/x/tools/txtar"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/internal/cueexperiment"
	"cuelang.org/go/internal/registrytest"
	"cuelang.org/go/internal/txtarfs"
)

// Note that these examples may not be runnable on pkg.go.dev,
// as they expect files to be present inside testdata.
// Using cue/load with real files on disk keeps the example realistic
// and enables the user to easily tweak the code to their needs.

func Example() {
	// Load the package "example" relative to the directory testdata/testmod.
	// Akin to loading via: cd testdata/testmod && cue export ./example
	insts := load.Instances([]string{"./example"}, &load.Config{
		Dir: filepath.Join("testdata", "testmod"),
		Env: []string{}, // or nil to use os.Environ
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

func Example_externalModules() {
	// setUpModulesExample starts a temporary in-memory registry,
	// populates it with an example module, and sets CUE_REGISTRY
	// to refer to it
	env, cleanup := setUpModulesExample()
	defer cleanup()

	insts := load.Instances([]string{"."}, &load.Config{
		Dir: filepath.Join("testdata", "testmod-external"),
		Env: env, // or nil to use os.Environ
	})
	inst := insts[0]
	if err := inst.Err; err != nil {
		fmt.Println(err)
		return
	}
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
	// Field string: hello, world
}

func setUpModulesExample() (env []string, cleanup func()) {
	registryArchive := txtar.Parse([]byte(`
-- foo.example_v0.0.1/cue.mod/module.cue --
module: "foo.example@v0"
-- foo.example_v0.0.1/bar/bar.cue --
package bar

value: "world"
`))

	registry, err := registrytest.New(txtarfs.FS(registryArchive), "")
	if err != nil {
		panic(err)
	}
	cleanups := []func(){registry.Close}
	env = append(env, "CUE_REGISTRY="+registry.Host()+"+insecure")
	dir, err := os.MkdirTemp("", "")
	if err != nil {
		panic(err)
	}
	env = append(env, "CUE_CACHE_DIR="+dir)
	oldModulesExperiment := cueexperiment.Flags.Modules
	cueexperiment.Flags.Modules = true
	cleanups = append(cleanups, func() {
		cueexperiment.Flags.Modules = oldModulesExperiment
	})
	return env, func() {
		for i := len(cleanups) - 1; i >= 0; i-- {
			cleanups[i]()
		}
	}
}
