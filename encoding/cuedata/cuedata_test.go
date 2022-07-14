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

package cuedata_test

import (
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/kylelemons/godebug/diff"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/load"
	"cuelang.org/go/encoding/cuedata"
	"cuelang.org/go/encoding/openapi"
	"cuelang.org/go/internal"
	cuediff "cuelang.org/go/internal/diff"
)

func TestParseDefinitions(t *testing.T) {
	testCases := []struct {
		in, out string
		config  *openapi.Config
		err     string
	}{
		{
			in:  "interpolation.cue",
			out: "interpolation_cuedata.cue",
		},
		{
			in:  "embed.cue",
			out: "embed_cuedata.cue",
		},
		{
			in:  "for.cue",
			out: "for_cuedata.cue",
		},
		{
			in:  "if.cue",
			out: "if_cuedata.cue",
		},
		{
			in:  "attribute.cue",
			out: "attribute_cuedata.cue",
		},
		{
			in:  "import.cue",
			out: "import_cuedata.cue",
		},
		{
			in:  "bottom.cue",
			out: "bottom_cuedata.cue",
		},
		{
			in:  "alias.cue",
			out: "alias_cuedata.cue",
		},
		{
			in:  "comments.cue",
			out: "comments_cuedata.cue",
		},
		{
			in:  "list.cue",
			out: "list_cuedata.cue",
		},
		{
			in:  "nums.cue",
			out: "nums_cuedata.cue",
		},
		{
			in:  "binaryexpr.cue",
			out: "binaryexpr_cuedata.cue",
		},
		{
			in:  "optional.cue",
			out: "optional_cuedata.cue",
		},
		{
			in:  "package.cue",
			out: "package_cuedata.cue",
		},
	}
	for _, tc := range testCases {
		t.Run(tc.out, func(t *testing.T) {
			filename := filepath.FromSlash(tc.in)

			inst := cue.Build(load.Instances([]string{filename}, &load.Config{
				Dir: "./testdata",
			}))[0]
			if inst.Err != nil {
				t.Fatal(errors.Details(inst.Err, nil))
			}

			// rewrite file to cuedata format
			f := internal.ToFile(inst.Value().Syntax(cue.All()))
			err := cuedata.NewEncoder().RewriteFile(f)
			if err != nil {
				errMsg := errors.Details(err, nil)
				if tc.err == errMsg {
					return
				}
				t.Fatal(errMsg)
			}

			rv, err := compile(*f)
			if err != nil {
				t.Fatal(err)
			}
			rw, err := formatValue(*rv)

			// load want file for diff
			wantFile := filepath.Join("testdata", tc.out)
			wb, err := ioutil.ReadFile(wantFile)
			if err != nil {
				t.Fatal(err)
			}

			// diff cue
			if d := diff.Diff(rw, string(wb)); d != "" {
				t.Errorf("files differ:\n%v", d)
			}

			// test encoding is idempotent
			err = cuedata.NewEncoder().RewriteFile(f)
			if err != nil {
				t.Fatal(errors.Details(err, nil))
			}

			rv, err = compile(*f)
			if err != nil {
				t.Fatal(err)
			}
			rw, err = formatValue(*rv)

			// diff again
			if d := diff.Diff(rw, string(wb)); d != "" {
				t.Errorf("files differ:\n%v", d)
			}

			// now test decoder, decode the encoded
			err = cuedata.NewDecoder().RewriteFile(f)
			if err != nil {
				t.Fatal(err)
			}
			dv, err := compile(*f)
			if err != nil {
				t.Fatal(err)
			}

			// diff decoded value with original value
			k, script := cuediff.Diff(*dv, inst.Value())
			if k != cuediff.Identity {
				t.Errorf("decoded does not match original.")
				cuediff.Print(os.Stdout, script)
			} else {
				// print decoded for dev review
				db, err := formatValue(*dv)
				if err != nil {
					t.Fatal(err)
				}
				log.Println(db)
			}

			// test decoder is idempotent, decode the decoded
			err = cuedata.NewDecoder().RewriteFile(f)
			if err != nil {
				t.Fatal(err)
			}
			dv, err = compile(*f)
			if err != nil {
				t.Fatal(err)
			}

			// diff double decoded value with original value
			k, script = cuediff.Diff(*dv, inst.Value())
			if k != cuediff.Identity {
				t.Errorf("decoded does not match original.")
				cuediff.Print(os.Stdout, script)
			}
		})
	}
}

func compile(f ast.File) (*cue.Value, error) {
	var r cue.Runtime
	inst, err := r.CompileFile(&f)
	if err != nil {
		return nil, err
	}
	v := inst.Value()
	return &v, nil
}

func formatValue(v cue.Value) (string, error) {
	n := v.Syntax(cue.All())
	cf := internal.ToFile(n)
	opts := []format.Option{format.Simplify()}
	b, err := format.Node(cf, opts...)
	return string(b), err
}
