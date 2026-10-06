// Copyright 2021 CUE Authors
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

package load

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-quicktest/qt"

	"cuelang.org/go/cue/ast"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/format"
	"cuelang.org/go/cue/parser"
	"cuelang.org/go/cue/token"
	"cuelang.org/go/internal/diff"
)

var testTagVars = map[string]TagVar{
	"now":      stringVar("2006-01-02T15:04:05.999999999Z"),
	"os":       stringVar("m1"),
	"cwd":      stringVar("home"),
	"username": stringVar("cueser"),
	"hostname": stringVar("cuebe"),
	"rand": {Func: func() (ast.Expr, error) {
		return ast.NewLit(token.INT, "112950970371208119678246559335704039641"), nil
	}},
}

func stringVar(s string) TagVar {
	return TagVar{Func: func() (ast.Expr, error) { return ast.NewString(s), nil }}
}

func TestTags(t *testing.T) {
	dir := t.TempDir()

	testCases := []struct {
		in  string
		out string
		err string
	}{{
		in: `
		rand: int    @tag(foo,var=rand)
		time: string @tag(bar,var=now)
		host: string @tag(bar,var=hostname)
		user: string @tag(bar,var=username)
		cwd:  string @tag(bar,var=cwd)
		`,

		out: `{
			rand: 112950970371208119678246559335704039641
			time: "2006-01-02T15:04:05.999999999Z"
			host: "cuebe"
			user: "cueser"
			cwd:  "home"
		}`,
	}, {
		in: `
		time: int @tag(bar,var=now)
		`,
		err: `time: conflicting values int and "2006-01-02T15:04:05.999999999Z" (mismatched types int and string)`,
	}, {
		// Auto inject only on marked places
		// TODO: Is this the right thing to do?
		in: `
			u1: string @tag(bar,var=username)
			u2: string @tag(bar)
			`,
		out: `{
			u1: "cueser"
            u2: string // not filled
        }`,
	}, {
		in: `
		u1: string @tag(bar,var=user)
		`,
		err: `tag variable 'user' not found`,
	}}

	for _, tc := range testCases {
		t.Run("", func(t *testing.T) {
			cfg := &Config{
				Dir: dir,
				Overlay: map[string]Source{
					filepath.Join(dir, "foo.cue"): FromString(tc.in),
				},
				TagVars: testTagVars,
			}
			b := Instances([]string{"foo.cue"}, cfg)[0]

			c := cuecontext.New()
			got := c.BuildInstance(b)
			switch err := got.Err(); {
			case (err == nil) != (tc.err == ""):
				t.Fatalf("error: got %v; want %v", err, tc.err)

			case err != nil:
				got := err.Error()
				if got != tc.err {
					t.Fatalf("error: got %v; want %v", got, tc.err)
				}

			default:
				want := c.CompileString(tc.out)
				if !got.Equals(want) {
					_, es := diff.Diff(got, want)
					b := &bytes.Buffer{}
					diff.Print(b, es)
					t.Error(b)
				}
			}
		})
	}

	// The syntax trees of the loaded instances hold each injected value once,
	// and loading does not modify the syntax trees given via [FromFile],
	// so the result is the same when loading a second time.
	syntaxCases := []struct {
		name    string
		overlay map[string]string
		// fromString gives the overlay via [FromString] rather than [FromFile],
		// so that the loader parses the files like it does files on disk.
		fromString bool
		args       []string
		want       string
	}{{
		name: "FromFile",
		overlay: map[string]string{
			"x.cue": "package x\n\na: string @tag(foo)\nb: a\n",
		},
		args: []string{"."},
		want: `== x.cue
package x

a: string & "bar" @tag(foo)
b: a
-- a refers to string & "bar"
`,
	}, {
		// Both instances share the file in the parent directory.
		name: "SharedParentFile",
		overlay: map[string]string{
			"x.cue":     "package x\n\na: string @tag(foo)\n",
			"sub/y.cue": "package x\n\nb: a\n",
		},
		args: []string{"./..."},
		want: `== x.cue
package x

a: string & "bar" @tag(foo)
== x.cue
package x

a: string & "bar" @tag(foo)
== y.cue
package x

b: a
-- a refers to string & "bar"
`,
	}, {
		// Both instances share the file parsed by the loader.
		name: "SharedParentFileFromString",
		overlay: map[string]string{
			"x.cue":     "package x\n\na: string @tag(foo)\n",
			"sub/y.cue": "package x\n\nb: a\n",
		},
		fromString: true,
		args:       []string{"./..."},
		want: `== x.cue
package x

a: string & "bar" @tag(foo)
== x.cue
package x

a: string & "bar" @tag(foo)
== y.cue
package x

b: a
-- a refers to string & "bar"
`,
	}, {
		// The value is injected via the second of the field's tags.
		name: "SharedParentFileMultipleTags",
		overlay: map[string]string{
			"x.cue":     "package x\n\na: string @tag(other) @tag(foo)\n",
			"sub/y.cue": "package x\n\nb: a\n",
		},
		fromString: true,
		args:       []string{"./..."},
		want: `== x.cue
package x

a: string & "bar" @tag(other) @tag(foo)
== x.cue
package x

a: string & "bar" @tag(other) @tag(foo)
== y.cue
package x

b: a
-- a refers to string & "bar"
`,
	}, {
		// A tag variable is injected alongside the other tag.
		name: "SharedParentFileTagVar",
		overlay: map[string]string{
			"x.cue":     "package x\n\na: string @tag(foo) @tag(v,var=os)\n",
			"sub/y.cue": "package x\n\nb: a\n",
		},
		fromString: true,
		args:       []string{"./..."},
		want: `== x.cue
package x

a: string & "bar" & "m1" @tag(foo) @tag(v,var=os)
== x.cue
package x

a: string & "bar" & "m1" @tag(foo) @tag(v,var=os)
== y.cue
package x

b: a
-- a refers to string & "bar" & "m1"
`,
	}}
	for _, tc := range syntaxCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{
				Dir: dir,
				Overlay: map[string]Source{
					filepath.Join(dir, "cue.mod", "module.cue"): FromString(`module: "mod.test", language: version: "v0.9.0"`),
				},
				Tags:    []string{"foo=bar"},
				TagVars: testTagVars,
			}
			for name, src := range tc.overlay {
				name = filepath.Join(dir, filepath.FromSlash(name))
				if tc.fromString {
					cfg.Overlay[name] = FromString(src)
					continue
				}
				f, err := parser.ParseFile(name, src)
				qt.Assert(t, qt.IsNil(err))
				cfg.Overlay[name] = FromFile(f)
			}
			Instances(tc.args, cfg) // the result is checked on the second load
			var buf strings.Builder
			for _, inst := range Instances(tc.args, cfg) {
				qt.Assert(t, qt.IsNil(inst.Err))
				for _, f := range inst.Files {
					b, err := format.Node(f)
					qt.Assert(t, qt.IsNil(err))
					fmt.Fprintf(&buf, "== %s\n%s", filepath.Base(f.Filename), b)
					ast.Walk(f, func(n ast.Node) bool {
						if id, ok := n.(*ast.Ident); ok && id.Node != nil {
							b, err := format.Node(id.Node)
							qt.Assert(t, qt.IsNil(err))
							fmt.Fprintf(&buf, "-- %s refers to %s\n", id.Name, b)
						}
						return true
					}, nil)
				}
			}
			qt.Assert(t, qt.Equals(buf.String(), tc.want))
		})
	}
}
