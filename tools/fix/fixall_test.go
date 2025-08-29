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

package fix

import (
	"fmt"
	"strings"
	"testing"

	"cuelang.org/go/cue/format"
	"cuelang.org/go/internal/cuetxtar"
	"cuelang.org/go/mod/modfile"
)

func TestInstances(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root: "./testdata",
		Name: "fixmod",
	}

	test.Run(t, func(t *cuetxtar.Test) {
		a := t.Instances("./...")
		opts := []Option{}
		if exp, ok := t.Value("exp"); ok {
			opts = append(opts, Experiments(strings.Split(exp, ",")...))
		}
		if str, ok := t.Value("upgrade"); ok {
			opts = append(opts, UpgradeVersion(str))
		}
		err := Instances(a, opts...)
		t.WriteErrors(err)
		for _, b := range a {
			// Output module file if it exists and was potentially modified
			if b.ModuleFile != nil {
				if data, err := modfile.Format(b.ModuleFile); err == nil {
					fmt.Fprintln(t, "---", "cue.mod/module.cue")
					fmt.Fprint(t, string(data))
				}
			}
			for _, f := range b.Files {
				b, _ := format.Node(f)
				fmt.Fprintln(t, "---", t.Rel(f.Filename))
				fmt.Fprint(t, string(b))
			}
		}
	})
}
