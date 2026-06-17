When you run into an issue with CUE, the most valuable thing you can attach to a
[bug report](https://github.com/cue-lang/cue/issues/new/choose) is a reproducer:
a small, self-contained artifact that lets anyone reproduce the problem on their
own machine, with no context on your setup.

The best way to write one is as a
[`cmd/testscript`](https://pkg.go.dev/github.com/rogpeppe/go-internal/cmd/testscript)
script: a sequence of commands run against a hermetic set of files held in a
single [`txtar`](https://pkg.go.dev/github.com/rogpeppe/go-internal/txtar)
archive. It is the format the CUE maintainers work in, so a good reproducer can
be dropped almost verbatim into the test suite, and become the regression test
that fixes the issue.

### Writing a reproducer

Commands come first; files follow, each delimited by a `-- filename --` line.
Pass the script as a filename, or pipe it on `stdin`:

```
testscript <<'EOD'
exec cue export
cmp stdout expect.golden

-- cue.mod/module.cue --
module: "mod.com"
-- x.cue --
package x

a: [1, 2, 3]
b: [for x in a if x > 1 {x}]
-- expect.golden --
{
    "a": [1, 2, 3],
    "b": [2, 3]
}
EOD
```

`exec` runs a program from your `PATH`; it is required because `cmd/testscript`
otherwise only exposes its own built-in commands. State the `cue` version you
tested against in the bug report (the issue template asks for it).

### Assert the behaviour you expect

Write the reproducer's assertions in terms of the behaviour you *expect* — the
output CUE *should* produce — using `cmp` against a golden file, as above. Run
against a `cue` that has the bug it fails, and the `cmp` diff is the
reproduction; once the bug is fixed it passes, so it can go straight into the
test suite as a regression test.

Encoding the current, buggy output instead makes the reproducer pass today, but
it then has to be rewritten once the issue is resolved, so it can never serve as
that regression test.

### Keep narrative out of the script

Don't add comments to the script explaining what the output is or what went
wrong; the assertions already make that unambiguous. Put your analysis, any
hunches about the cause, and links to related issues in the bug report instead.

### Other assertions

`cmp` is the workhorse, but `cmd/testscript` has more built-ins for asserting
directly in the script — for example `! exec cue vet` (the command must fail),
`stdout 'regexp'` / `! stdout 'regexp'` (output must / must not match), and
`cmpenv` (like `cmp`, but expands `$VARS`). See the
[`testscript` documentation](https://pkg.go.dev/github.com/rogpeppe/go-internal/testscript)
for the full set.

### Omitting `go.sum`

`go.sum` files make reproducers unwieldy and are unnecessary here: reproducers
resolve through the module proxy, a sufficiently reliable source for this
purpose. Omit `go.sum` and make `go mod tidy` the first command to regenerate it
(since [golang/go#40728](https://go.dev/issue/40728) a required `go.sum` may not
be absent):

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

### Other ways to reproduce

If your problem already lives in a directory of files,
[`txtar-c`](https://pkg.go.dev/github.com/rogpeppe/go-internal/cmd/txtar-c)
prints that directory as a `txtar` archive ready to drop into a script (and
[`txtar-x`](https://pkg.go.dev/github.com/rogpeppe/go-internal/cmd/txtar-x) does
the reverse).

If a problem genuinely can't be reduced to a hermetic archive — it depends on a
large external repository, say — fall back to a sequence of shell commands that
clones the repository at a **specific commit** (not a moving branch) in a fresh
temporary directory, and include the output you saw so others can confirm it.
Prefer a `cmd/testscript` reproducer whenever the problem can be expressed as
one.
