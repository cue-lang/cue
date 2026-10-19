# CUE syntax corpus

Each `.cue` file in this directory shows one shape of CUE syntax. Tools that
parse or highlight CUE, such as editor grammars, can test themselves against
it. `TestSyntaxCorpus` in `cue/parser/syntax_test.go` checks every file
against the parser.

## Files

File names are in snake_case and say what the file shows, as in
`alias_postfix_paren.cue`.

Each file starts with a header stating what the parser does with it at each
minor language version, from the earliest a module can declare, v0.8.0, to
the current one. The header has one line for each run of versions with the
same outcome:

```cue
// versions: v0.8.0..v0.17.0; outcome: error 3:2: postfix alias syntax requires @experiment(aliasv2)
// versions: v0.18.0..; outcome: ok
a~(X): {b: 1}
c: X.b
```

- `versions` is a minor version, or a range of them. A range without an end
  extends to the current version and beyond.
- `experiments`, when present, names the experiments the file enables with
  `@experiment(...)`, and the line gives the outcome with them on, at the
  versions where they exist. A line without it gives the outcome with the
  file's `@experiment` attributes removed.
- `outcome` is `ok`, or `error <line>:<col>: <message>`: the position and
  message of the parser's first error, counting the header's lines.

The test fails when a header differs from what the parser does. Rather than
editing headers by hand, run

```sh
CUE_UPDATE=1 go test ./cue/parser -run TestSyntaxCorpus
```

which rewrites them.

The parser decides what is valid, including where the
[language specification](../../../../doc/ref/spec.md) disagrees with it: for
example, an alias comes before an optional or required marker, as in
`a~(X)?: int`. Two forms the parser accepts are left out: `#` alone as a
name, which the specification does not allow, and slices, which it does not
define. The parser may also accept syntax with an experiment off and leave
its rejection to the compiler, as for `try`; the header records what the
parser does.

## Keeping it current

A change that adds or changes CUE syntax adds or updates the corpus files for
that syntax in the same change: a new file, enabling its experiment if the
syntax has one, followed by the update above. A change to what the parser
accepts at some language version, or to the earliest version a module can
declare, shows up as headers that the update rewrites.

## Using the corpus

Copy the `.cue` files from a tagged release of
[cue-lang/cue](https://github.com/cue-lang/cue), and record which tag they
came from, so that updating the copy is a deliberate step:

```sh
git clone --branch <tag> https://github.com/cue-lang/cue
cp cue/cue/parser/testdata/syntax/*.cue <your-copy>/
```

A tool that cannot see a file's language version or experiments should handle
every file with an `outcome: ok` line without errors. It should ideally
reject the others, since no language version accepts them. A tool that knows
the language version and experiments may follow the header more closely. A
tool that rejects a file may also compare its own error position and message
with the header; they need not match exactly.
