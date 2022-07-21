// Copyright 2022 CUE Authors
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

package export_test

import (
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/core/export"
	"cuelang.org/go/internal/core/runtime"
	"cuelang.org/go/internal/cuetest"
	"cuelang.org/go/internal/cuetxtar"
	"cuelang.org/go/internal/types"
	"cuelang.org/go/internal/value"
	"github.com/rogpeppe/go-internal/txtar"
)

func TestSelfContained(t *testing.T) {
	test := cuetxtar.TxTarTest{
		Root:   "./testdata/selfcontained",
		Update: cuetest.UpdateGoldenFiles,
	}

	r := cuecontext.New()

	test.Run(t, func(t *cuetxtar.Test) {
		a := t.ValidInstances()

		v := r.BuildInstance(a[0])
		if err := v.Err(); err != nil {
			t.Fatal(err)
			return
		}

		p, ok := t.Value("path")
		if ok {
			v = v.LookupPath(cue.ParsePath(p))
		}

		var tValue types.Value
		v.Core(&tValue)

		self := *export.All
		self.SelfContained = true

		w := t.Writer("default")

		test := func() {
			file, errs := self.Def(tValue.R, "", tValue.V)

			errors.Print(w, errs, nil)
			_, _ = w.Write(formatNode(t.T, file))
		}
		test()

		if _, ok := t.Value("inlineImports"); ok {
			w = t.Writer("expand_imports")
			self.InlineImports = true
			test()
		}
	})
}

// TestSC is for debugging purposes. Do not delete.
func TestSC(t *testing.T) {
	in := `
-- cue.mod/module.cue --
module: "example.com/a"

-- in.cue --
	`
	if strings.HasSuffix(strings.TrimSpace(in), ".cue --") {
		t.Skip()
	}

	a := txtar.Parse([]byte(in))
	instance := cuetxtar.Load(a, "/tmp/test")[0]
	if instance.Err != nil {
		t.Fatal(instance.Err)
	}

	r := runtime.New()

	v, err := r.Build(nil, instance)
	if err != nil {
		t.Fatal(err)
	}

	adt.Verbosity = 1
	defer func() { adt.Verbosity = 0 }()

	e := eval.New(r)
	ctx := e.NewContext(v)
	v.Finalize(ctx)

	var tValue types.Value
	value.Make(ctx, v).LookupPath(cue.ParsePath("a.b")).Core(&tValue)
	v = tValue.V

	self := export.All
	self.SelfContained = true

	file, errs := self.Def(r, "", v)
	if errs != nil {
		t.Fatal(errs)
	}

	b, _ := format.Node(file)
	t.Error(string(b))
}
