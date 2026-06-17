When you run into an issue with CUE, the most valuable thing you can attach to a
[bug report](https://github.com/cue-lang/cue/issues/new/choose) is a reproducer:
a small, self-contained artifact that lets anyone — with no context on your
setup — reproduce the problem exactly on their own machine.

This page describes how to write a good reproducer. Two things matter as much as
the reproduction itself:

1. **Use `cmd/testscript`.** It is the format the CUE maintainers work in. A
   reproducer written as a testscript can be dropped almost verbatim into the
   test suite, which means your report can become the regression test that fixes
   it.
2. **Write the reproducer so it _passes_.** Encode the behaviour you _expect_,
   not the broken behaviour you are seeing. The failure then demonstrates
   exactly how reality diverges from that expectation — and the day the bug is
   fixed, the test goes green with no further edits.

### Write `cmd/testscript` reproducers

[`cmd/testscript`](https://pkg.go.dev/github.com/rogpeppe/go-internal/cmd/testscript)
runs a small script of commands against a hermetic set of files, all held in a
single [`txtar`](https://pkg.go.dev/github.com/rogpeppe/go-internal/txtar)
archive. Commands come first; files follow, each delimited by a `-- filename --`
line.

Pass it a filename, or pipe the script on `stdin`:

```
testscript <<'EOD'
exec cue export
cmp stdout expect.golden

-- cue.mod/module.cue --
module: "mod.com"
-- x.cue --
package x

a: 41
a: 42
-- expect.golden --
... what you expect to happen ...
EOD
```

Notes:

* `exec cue ...` runs the `cue` on your `PATH`. The `exec` prefix is required:
  `cmd/testscript` only exposes its own built-in commands, so external programs
  must be run via `exec`.
* The whole thing is one file. There is nothing to extract, no directory to
  create, and no risk that an investigator runs a different version of your
  inputs than the one you intended.
* State the `cue` version you tested against in the bug report (the issue
  template asks for it).

### Make the expectation a passing assertion

This is the most important part, and the one most reproducers get wrong.

The instinct is to write down what CUE _currently_ does — to bake the buggy
output into a golden file or an assertion. Don't. A reproducer that encodes the
broken behaviour passes today and would have to be _rewritten_ once the bug is
fixed, so it can never become the regression test.

Instead, write the assertion so that it states the **correct** result — the
output you believe CUE _should_ produce. Use `cmp` against a golden file that
holds that expected output:

```
exec cue export
cmp stdout expect.golden

-- cue.mod/module.cue --
module: "example.com/x"
-- x.cue --
package x

#A: {
	a: string
	b: *a | string
}

s: [Name=string]: #A & {
	a: Name
}

s: foo: b: "123"
s: bar: _

foo: [
	for _, a in s if a.b != _|_ {a},
]
-- expect.golden --
{
    "s": {
        "foo": {
            "a": "foo",
            "b": "123"
        },
        "bar": {
            "a": "bar",
            "b": "bar"
        }
    },
    "foo": [
        {
            "a": "foo",
            "b": "123"
        },
        {
            "a": "bar",
            "b": "bar"
        }
    ]
}
```

Run it, and `cmp` reports precisely where reality diverges from the
expectation — here, the second comprehension element is missing:

```
$ testscript repro.txtar
> exec cue export
> cmp stdout expect.golden
[diff -stdout +expect.golden]
 ...
     "foo": [
         {
             "a": "foo",
             "b": "123"
+        },
+        {
+            "a": "bar",
+            "b": "bar"
         }
     ]
 }

FAIL: .../script.txtar:2: stdout and expect.golden differ
```

The report now reads as a clear claim — "this should produce X" — and the
failure _is_ the bug. When someone fixes it, the same file passes unchanged, so
it can be added to the suite as-is.

### Let the assertions speak — don't narrate

Do **not** add comments to the script describing what the output is, what you
expected, or what went wrong. The whole point of the `cmp` (and friends) is to
make the expected and actual results unambiguous; a prose comment can only
duplicate that information, drift out of date, or contradict the golden file.
Keep the script to commands and files. Put any narrative — your analysis, what
you think the cause might be, links to related issues — in the bug report
itself, not in the reproducer.

### Other useful assertions

`cmp` is the workhorse, but `cmd/testscript` has a range of built-ins for
expressing expectations directly in the script — for example:

* `! exec cue vet` — assert that a command _fails_.
* `stdout 'some regexp'` / `stderr 'some regexp'` — assert output matches.
* `! stdout 'pattern'` — assert output does _not_ match.
* `cmpenv` — like `cmp`, but expands `$VARS` in the golden file.

See the [`testscript` documentation](https://pkg.go.dev/github.com/rogpeppe/go-internal/testscript)
for the full set.

### Omitting `go.sum`

`go.sum` files make `txtar` reproducers unwieldy and are unnecessary here:
reproducers resolve through the module proxy, which we are comfortable treating
as a sufficiently reliable source for this purpose. Omit `go.sum` and add a
`go mod tidy` step as the first command to regenerate it (since
[golang/go#40728](https://go.dev/issue/40728) it is an error for a required
`go.sum` to be absent):

```
go mod tidy
go run main.go

-- cue.mod/module.cue --
module: "example.com/x"
-- go.mod --
module example.com/x

go 1.16

require cuelang.org/go v0.4.0
-- main.go --
package main

import (
	"fmt"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
)

func main() {
	ctx := cuecontext.New()
	v := ctx.CompileString("x: 5")
	fmt.Printf("x: %v\n", v.LookupPath(cue.MakePath(cue.Str("x"))))
}
-- expect.golden --
x: 5
```

### Capturing an existing setup

If your problem already lives in a directory of files, you don't have to
assemble the archive by hand.
[`txtar-c`](https://pkg.go.dev/github.com/rogpeppe/go-internal/cmd/txtar-c)
prints a directory as a `txtar` archive (and
[`txtar-x`](https://pkg.go.dev/github.com/rogpeppe/go-internal/cmd/txtar-x) does
the reverse). Run `txtar-c` in the directory, then add your commands and the
expected-output golden at the top to turn it into a `cmd/testscript` reproducer.

### When a reproducer genuinely won't reduce

Occasionally a problem can't be reduced to a hermetic archive — it depends on a
large external repository, for instance. In that case, provide a sequence of
shell commands that clones the relevant repository at a **specific commit** (not
a moving branch reference) inside a fresh temporary directory, and include the
output you saw locally so others can confirm it reproduces. This is a last
resort: prefer a `cmd/testscript` reproducer whenever the problem can be
expressed as one.
