# CUE syntax corpus

Each `.cue` file under this directory shows one shape of CUE syntax. Tools
that parse or highlight CUE, such as editor grammars, can test themselves
against it. `TestSyntaxCorpus` in `cue/parser/syntax_test.go` checks every
file against the parser at the current language version.

## Files

The directory holding a file gives its class:

- `pass/`: accepted by default at the current language version. The file
  enables no experiment.
- `preview/`: syntax of a preview experiment, which the file enables with an
  `@experiment(...)` attribute. The parser accepts it with the experiment on.
  It may also accept some of it with the experiment off, leaving that check
  to the compiler, as for `try`.
- `fail/`: rejected at the current language version. Its first line is
  `// error: <line>:<col>: <text>`: parsing fails, and the first error is at
  that line and column, with a message containing `<text>`. The file may
  enable an experiment, when the shape is rejected even with it on.

File names are in snake_case and say what the file shows, as in
`pass/alias_postfix_paren.cue`.

The parser decides what is valid, including where the
[language specification](../../../../doc/ref/spec.md) disagrees with it: for
example, an alias comes before an optional or required marker, as in
`a~(X)?: int`. Two forms the parser accepts are left out: `#` alone as a
name, which the specification does not allow, and slices, which it does not
define.

## Keeping it current

A change that adds or changes CUE syntax adds or updates the corpus files for
that syntax in the same change:

- **New syntax**: add files to `pass/`, or to `preview/` if it is behind an
  experiment.
- **Experiment stabilized**: move its files from `preview/` to `pass/` and
  remove the `@experiment` attribute.
- **Syntax removed**: move its files to `fail/` and add the `// error:` line.

## Using the corpus

Copy the three directories from a tagged release of
[cue-lang/cue](https://github.com/cue-lang/cue), and record which tag they
came from, so that updating the copy is a deliberate step:

```sh
git clone --branch <tag> https://github.com/cue-lang/cue
cp -R cue/cue/parser/testdata/syntax/{pass,preview,fail} <your-copy>/
```

- **`pass/` and `preview/` files**: a tool that cannot see a file's language
  version or experiments should handle all of them without errors.
- **`fail/` files**: show syntax the current language version rejects. A tool
  may still accept some of them. For example, the `fail/prefix_alias_*` files
  are valid at language versions before v0.18.0. A tool that rejects a file
  may also compare its own error position and message with the first line;
  they need not match exactly.
