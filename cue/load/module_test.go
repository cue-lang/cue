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

package load_test

import (
	"fmt"
	"io/fs"
	"testing"

	"cuelabs.dev/go/oci/ociregistry/ociclient"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/internal/cuetxtar"
	"cuelang.org/go/internal/registrytest"
	"cuelang.org/go/internal/txtarfs"
)

func TestModuleFetch(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root: "./testdata/testfetch",
		Name: "modfetch",
	}
	test.Run(t, func(t *cuetxtar.Test) {
		rfs, err := fs.Sub(txtarfs.FS(t.Archive), "_registry")
		if err != nil {
			t.Fatal(err)
		}
		r, err := registrytest.New(rfs, "")
		if err != nil {
			t.Fatal(err)
		}
		defer r.Close()
		reg, err := ociclient.New(r.Host(), &ociclient.Options{
			Insecure: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		t.LoadConfig.Registry = reg
		ctx := cuecontext.New()
		insts := t.RawInstances()
		if len(insts) != 1 {
			t.Fatalf("wrong instance count; got %d want 1", len(insts))
		}
		inst := insts[0]
		if inst.Err != nil {
			errors.Print(t.Writer("error"), inst.Err, &errors.Config{
				ToSlash: true,
			})
			return
		}
		v := ctx.BuildInstance(inst)
		if err := v.Validate(); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(t, "%v\n", v)
	})
}
