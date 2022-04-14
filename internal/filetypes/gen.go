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

//go:build ignore
// +build ignore

package main

import (
	"io/ioutil"
	"log"
	"os"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/encoding/gocode"
)

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		log.Fatal(err)
	}

	inst := cue.Build(load.Instances([]string{"types.cue"}, &load.Config{
		Dir:        cwd,
		ModuleRoot: cwd,
		Module:     "cuelang.org/go/cue/build",
	}))[0]
	if inst.Err != nil {
		log.Fatal(inst.Err)
	}

	b, err := gocode.Generate(".", inst, &gocode.Config{})
	if err != nil {
		log.Fatal(err)
	}

	if err := ioutil.WriteFile("types.go", b, 0644); err != nil {
		log.Fatal(err)
	}
}
