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

package protobuf

import (
	"bytes"
	"flag"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"testing"

	"cuelang.org/go/cue/format"
	"github.com/kr/pretty"
)

var update *bool = flag.Bool("update", false, "update the test output")

func TestParseDefinitions(t *testing.T) {
	testCases := []string{
		"networking/v1alpha3/gateway.proto",
		"mixer/v1/attributes.proto",
		"mixer/v1/config/client/client_config.proto",
	}
	for _, file := range testCases {
		t.Run(file, func(t *testing.T) {
			filename := filepath.Join("testdata", filepath.FromSlash(file))
			c := &Config{
				Paths: []string{"testdata"},
			}

			out := &bytes.Buffer{}

			if f, err := Parse(filename, nil, c); err != nil {
				fmt.Fprintln(out, err)
			} else {
				format.Node(out, f)
			}

			wantFile := filepath.Join("testdata", filepath.Base(file)+".out.cue")
			if *update {
				ioutil.WriteFile(wantFile, out.Bytes(), 0644)
				return
			}

			b, err := ioutil.ReadFile(wantFile)
			if err != nil {
				t.Fatal(err)
			}

			if desc := pretty.Diff(out.String(), string(b)); len(desc) > 0 {
				t.Errorf("files differ:\n%v", desc)
			}
		})
	}
}
