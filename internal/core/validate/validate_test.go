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

package validate

import (
	"fmt"
	"strings"
	"testing"

	"cuelang.org/go/cue/errors"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/internal/core/adt"
	"cuelang.org/go/internal/core/compile"
	"cuelang.org/go/internal/core/eval"
	"cuelang.org/go/internal/core/runtime"
	"github.com/google/go-cmp/cmp"
)

func TestValidate(t *testing.T) {
	testCases := []struct {
		desc   string
		in     string
		out    string
		lookup string
		cfg    *Config
	}{{
		desc: "no error, but not concrete, even with definition label",
		cfg:  &Config{Concrete: true},
		in: `
		#foo: { use: string }
		`,
		lookup: "#foo",
		out:    "incomplete\n#foo.use: incomplete value string",
	}, {
		desc: "definitions not considered for completeness",
		cfg:  &Config{Concrete: true},
		in: `
		#foo: { use: string }
		`,
	}, {
		desc: "hidden fields not considered for completeness",
		cfg:  &Config{Concrete: true},
		in: `
		_foo: { use: string }
		`,
	}, {
		desc: "hidden fields not considered for completeness",
		in: `
		_foo: { use: string }
		`,
	}, {
		desc: "evaluation error at top",
		in: `
		1 & 2
		`,
		out: "eval\nincompatible values 2 and 1",
	}, {
		desc: "evaluation error in field",
		in: `
		x: 1 & 2
		`,
		out: "eval\nx: incompatible values 2 and 1",
	}, {
		desc: "first error",
		in: `
		x: 1 & 2
		y: 2 & 4
		`,
		out: "eval\nx: incompatible values 2 and 1",
	}, {
		desc: "all errors",
		cfg:  &Config{AllErrors: true},
		in: `
		x: 1 & 2
		y: 2 & 4
		`,
		out: `eval
x: incompatible values 2 and 1
y: incompatible values 4 and 2`,
	}, {
		desc: "incomplete",
		cfg:  &Config{Concrete: true},
		in: `
		y: 2 + x
		x: string
		`,
		out: "incomplete\ny: non-concrete value string in operand to +:\n    test:2:6",
	}, {
		desc: "allowed incomplete cycle",
		in: `
		y: x
		x: y
		`,
	}, {
		desc: "allowed incomplete when disallowing cycles",
		cfg:  &Config{DisallowCycles: true},
		in: `
		y: string
		x: y
		`,
	}, {
		desc: "disallow cycle",
		cfg:  &Config{DisallowCycles: true},
		in: `
		y: x + 1
		x: y - 1
		`,
		out: "cycle\ncycle error",
	}, {
		desc: "disallow cycle",
		cfg:  &Config{DisallowCycles: true},
		in: `
		a: b - 100
		b: a + 100
		c: [c[1], c[0]]		`,
		out: "cycle\ncycle error",
	}, {
		desc: "treat cycles as incomplete when not disallowing",
		cfg:  &Config{},
		in: `
		y: x + 1
		x: y - 1
		`,
	}, {
		// Note: this is already handled by evaluation, as terminal errors
		// are percolated up.
		desc: "catch most serious error",
		cfg:  &Config{Concrete: true},
		in: `
		y: string
		x: 1 & 2
		`,
		out: "eval\nx: incompatible values 2 and 1",
	}}

	r := runtime.New()
	ctx := eval.NewContext(r, nil)

	for _, tc := range testCases {
		t.Run(tc.desc, func(t *testing.T) {
			f, err := parser.ParseFile("test", tc.in)
			if err != nil {
				t.Fatal(err)
			}
			v, err := compile.Files(nil, r, f)
			if err != nil {
				t.Fatal(err)
			}
			ctx.Unify(ctx, v, adt.Finalized)
			if tc.lookup != "" {
				v = v.Lookup(adt.MakeIdentLabel(r, tc.lookup))
			}

			b := Validate(ctx, v, tc.cfg)

			w := &strings.Builder{}
			if b != nil {
				fmt.Fprintln(w, b.Code)
				errors.Print(w, b.Err, nil)
			}

			got := strings.TrimSpace(w.String())
			if tc.out != got {
				t.Error(cmp.Diff(tc.out, got))
			}
		})
	}
}
