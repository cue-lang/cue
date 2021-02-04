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

package gocode

import (
	"bytes"
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/kylelemons/godebug/diff"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/internal/cuetest"
	_ "cuelang.org/go/pkg"
)

func TestGenerate(t *testing.T) {
	dirs, err := ioutil.ReadDir("testdata")
	if err != nil {
		t.Fatal(err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}

	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		t.Run(d.Name(), func(t *testing.T) {
			dir := filepath.Join(cwd, "testdata")
			pkg := "." + string(filepath.Separator) + d.Name()
			inst := cue.Build(load.Instances([]string{pkg}, &load.Config{
				Dir:        dir,
				ModuleRoot: dir,
				Module:     "cuelang.org/go/encoding/gocode/testdata",
			}))[0]
			if err := inst.Err; err != nil {
				t.Fatal(err)
			}

			goPkg := "./testdata/" + d.Name()
			b, err := Generate(goPkg, inst, nil)
			if err != nil {
				t.Fatal(errStr(err))
			}

			goFile := filepath.Join("testdata", d.Name(), "cue_gen.go")
			if cuetest.UpdateGoldenFiles {
				_ = ioutil.WriteFile(goFile, b, 0644)
				return
			}

			want, err := ioutil.ReadFile(goFile)
			if err != nil {
				t.Fatal(err)
			}

			if d := diff.Diff(string(want), string(b)); d != "" {
				t.Errorf("files differ:\n%v", d)
			}
		})
	}
}

func errStr(err error) string {
	if err == nil {
		return "nil"
	}
	buf := &bytes.Buffer{}
	errors.Print(buf, err, nil)
	r := regexp.MustCompile(`.cue:\d+:\d+`)
	return r.ReplaceAllString(buf.String(), ".cue:x:x")
}
