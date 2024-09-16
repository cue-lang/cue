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

package main

import (
	"log"
	"os"
	"path/filepath"

	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/encoding/gocode"
	_ "cuelang.org/go/pkg"
)

func main() {
	insts := load.Instances([]string{"./..."}, &load.Config{
		Dir: "testdata",
	})
	ctx := cuecontext.New()
	for _, inst := range insts {
		if err := inst.Err; err != nil {
			log.Fatal(err)
		}

		b, err := gocode.Generate(inst.Dir, ctx.BuildInstance(inst), nil)
		if err != nil {
			log.Fatal(err)
		}

		goFile := filepath.Join(inst.Dir, "cue_gen.go")
		if err := os.WriteFile(goFile, b, 0666); err != nil {
			log.Fatal(err)
		}
	}
}
