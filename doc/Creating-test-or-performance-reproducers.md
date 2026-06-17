When you run into an issue with CUE, reducing this down to a simple example helps others who have no context quickly understand where the issue might be. 

But sometimes that's simply not possible: it's too complex to reduce, spread across multiple files, etc. In such cases, providing a means by which someone can easily and precisely reproduce your setup locally on their machine is critical. These details should be included in a [GitHub bug report](https://github.com/cue-lang/cue/issues/new/choose). 

Here are two ways in which you can provide such steps:

### Sequences of shell commands

One way is to provide a sequence of shell commands that can be copy-pasted locally. For example:

```
cd $(mktemp -d)
(
set -e
git clone https://github.com/play-with-go/preguide
cd preguide
git checkout 9dcd6cce2ddbe9e8d44649815bb6916830da8cb7
cue def
)
```

Points to note from this approach:

* the entire block above can be copy-pasted an run locally - no need to remove any shell prompt text like `$`
* the reproduction is run within a new temporary directory, `cd $(mktemp -d)`
* the main command block is run in a subshell, denoted by the outer parentheses. `set -e` causes the subshell to exit on any subsequent command errors
* we `git checkout` to a specific commit so anyone investigating can be sure of running exactly the same code (otherwise we can't be sure we're not running another commit, because branch references like `main`/`master` etc move)
* the version of `cue` is given by the GitHub bug report issue template (which should also be completed)

Best practice is to include the output from copy-pasting this same block locally on your machine. That way you can be sure the reproduction is a genuine reproduction.

### Creating a `txtar` archive

Another way is to provide an entirely hermetic reproduction file in the form of a [`txtar`](https://pkg.go.dev/github.com/rogpeppe/go-internal/txtar) archive. `txtar` is a trivial text-based file archive format. You can use [`txtar-c`](https://pkg.go.dev/github.com/rogpeppe/go-internal/cmd/txtar-c) to create a `txtar` file from an existing directory structure of files. 

To quickly demonstrate the use of `txtar-c`, let's create a setup locally which contains a problem we want to report:

```
cd $(mktemp -d)
(
set -e
cue mod init mod.com
cat <<EOD > x.cue
package x

a: 41
a: 42
EOD
)
```

If we now run `txtar-c` we get the following output:

```
-- cue.mod/module.cue --
module: "mod.com"
-- x.cue --
package x

a: 41
a: 42
```

Each file in the archive is delineated by `-- filename --` blocks. You can optionally put the commands you ran above the first file in the archive:

```
exec cue def

-- cue.mod/module.cue --
module: "mod.com"
-- x.cue --
package x

a: 41
a: 42
```

Alternatively, simply include the command(s) run in a separate part of the bug report.

Given the above report (assuming the bug report told use we should test against `cue v0.2.2`) we should see the output:

```
a: conflicting values 41 and 42:
    ./x.cue:3:4
    ./x.cue:4:4
```

Someone investigating the problem can then trivially run [`txtar-x`](https://pkg.go.dev/github.com/rogpeppe/go-internal/cmd/txtar-x) to do the reverse, expanding this archive, at which point they will have an identical local setup and should be able to reproduce the problem.

### Testing a repro using `cmd/testscript`

Alternatively, [`cmd/testscript`](https://pkg.go.dev/github.com/rogpeppe/go-internal/cmd/testscript) can be passed a filename argument that contains the reproducer, or the contents passed directly as `stdin`:

```
$ testscript <<'EOD'
exec cue def

-- cue.mod/module.cue --
module: "mod.com"
-- x.cue --
package x

a: 41
a: 42
EOD
```

Note: the `exec` command is used here because `cmd/testscript` only makes the default [`testscript`](https://pkg.go.dev/github.com/rogpeppe/go-internal/testscript) commands available. `exec cue` runs `cue` from your `PATH`.

### Comparing against golden files

In cases where you need to compare output vs a golden file/content, you can use [`testscript`](https://pkg.go.dev/github.com/rogpeppe/go-internal/testscript)'s `cmp` and friends. For example, given the following repro:

```
exec cue version
exec cue export
cmp stdout stdout.golden

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
-- stdout.golden --
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

the output using `cmd/testscript` is:

```
$ testscript repro.txt

> exec cue version
[stdout]
cue version 0.3.0-beta.5 linux/amd64
> exec cue export
[stdout]
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
        }
    ]
}
> cmp stdout stdout.golden
[diff -stdout +stdout.golden]
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
+        },
+        {
+            "a": "bar",
+            "b": "bar"
         }
     ]
 }

FAIL: /tmp/testscript922374216/repro.txt/script.txt:3: stdout and stdout.golden differ
```

### `go.sum` files in `txtar` repros

`go.sum` files make `txtar` repros incredibly unwieldy. There are also unnecessary for the purposes of the repro: (almost) all repros necessarily resolve via the proxy and we are comfortable that is a sufficiently reliable source of code (for repros) so as not to require an independent check via `go.sum`.

`go.sum` files can therefore be ommited from repros, and instead a `go mod tidy` step included as the first command in order to populate the now missing `go.sum` file (following https://github.com/golang/go/issues/40728 it is an error for `go.sum` to not exist if it is required).

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
-- stdout.golden --
x: 5
```
