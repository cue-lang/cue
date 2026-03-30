# Inline Test Attributes — `cue/testdata/inlinetest`

This directory contains the reference test suite for the **inline assertion**
system implemented in `internal/cuetxtar/inline.go`. Each `.txtar` archive
here exercises a distinct feature area.

---

## Overview

Inline assertion mode replaces golden-file comparison with `@test(...)`
attributes written directly in the `.cue` source of a txtar archive.  The
runner (`cuetxtar.RunInlineTests`, called from `TestEvalV3`) detects inline
mode automatically: if any `.cue` file in the archive contains at least one
`@test(...)` attribute, the archive is processed as an inline test.  Archives
with no `@test` attributes continue to use golden-file comparison unchanged.

There are two syntactic positions for `@test(...)` attributes:

---

## Inline form

A `@test(...)` *field attribute* placed on a top-level field makes that field a
self-contained test case.  The field name becomes the sub-test name.

```cue
// basic.txtar — in.cue

simpleInt: 42             @test(eq, 42)
simpleStr: "hello"        @test(eq, "hello")
errField:  1 & 2          @test(err)
kindInt:   int            @test(kind=int)
```

---

## File-level form

A `@test(eq, VALUE)` *decl attribute* at the top level of a `.cue` file checks
the **entire file's** evaluated value against `VALUE`.  All fields are
implicitly covered — no per-field `@test` is required.

```cue
// decl_eq.txtar — in.cue

a: 1
b: a + 1
c: {
    x: "hello"
    y: b
}
@test(eq, {
    a: 1
    b: 2
    c: {
        x: "hello"
        y: 2
    }
})
```

This form is useful when:

- Pattern constraints contribute to the output (e.g. `{[X=string]: baz: X}`),
  so per-field annotation would miss the constraint's effect.
- The test is a simple whole-file check and per-field annotation would be
  redundant.
- Individual fields may still carry field-level `@test` attributes alongside
  the file-level `@test`; both are checked.

---

## Directives

### `eq` — exact equality

```cue
simple: 42 @test(eq, 42)                          // inline: argument is a CUE expression
complex: {a: 1} @test(file, "expected/r.cue")     // inline: expected value in a txtar section
```

Comparison uses `internal/core/diff`.  Insignificant differences (field
ordering, let-binding substitution, comment presence) are ignored.  Error
*message text* is ignored; error *kind* and *code* are compared.

### `leq` — subsumption constraint

```cue
count: 5 @test(leq, int)
```

Asserts `evaluate(field) ⊑ constraint`.  Useful for type-level assertions
without pinning an exact value.

### `err` — error assertion

```cue
errField:  1 & 2          @test(err)
errCycle:  errCycle + 1   @test(err, code=cycle)
errMsg:    1 & 2          @test(err, contains="conflicting")
```

Optional arguments:

| Argument | Meaning |
|----------|---------|
| `code=<c>` | error code must match (`cycle`, `eval`, `incomplete`, …) |
| `contains="s"` | error message must contain substring `s` |
| `any` | at least one *descendant* has the error (requires `code=`) |
| `path=(p\|q)` | error exists at one of the listed paths |

### `kind` — value kind

```cue
kindInt:  int    @test(kind=int)
kindMixed: int | string  @test(kind=int|string)
```

### `closed` — struct openness

```cue
closedTrue:  close({x: 1}) @test(closed)
closedFalse: {x: 1}        @test(closed=false)
```

### `debugCheck` — debug-printer output

```cue
debugScalar: 42    @test(debugCheck, "(int){ 42 }")
debugStruct: {a: 1} @test(debugCheck, "(struct){\n  a: (int){ 1 }\n}")
```

Compares the string output of `internal/core/debug`'s printer applied to the
evaluated value.  Useful for verifying internal representation details that `eq`
does not capture.

### `skip` — skip a test case

```cue
wip: someExpr @test(skip, why="not yet implemented")
```

A versioned form `skip:v3` skips only under evaluator version `v3`.

### `ignore` — opt out

```cue
sharedFixture: {x: 42} @test(ignore)   // field-attribute form
helper: {
    @test(ignore)                        // decl-attribute form
    y: 10
}
```

A field with `@test(ignore)` is not run as a sub-test.  A `.cue` file with
*no* `@test` attributes at all is treated as a pure fixture file.

### `permute` — field-order independence

Asserts that the marked fields produce the same result in all N! orderings.

```cue
// Field attribute form: mark each field to include in the permutation set.
permuteStruct: {
    x: y + 1 @test(permute)
    y: 2     @test(permute)
} @test(eq, {x: 3, y: 2}) @test(permuteCount, 2)
```

### `permuteCount` — verify permutation count

```cue
permuteStruct: {
    a: b + c @test(permute)
    b: 1     @test(permute)
    c: 2     @test(permute)
} @test(eq, {a: 3, b: 1, c: 2}) @test(permuteCount, 6)
```

Placed on the struct containing `@test(permute)` fields.  Asserts that the
total number of evaluated permutations equals `N`.  Auto-updated by
`CUE_UPDATE=1`.

### `desc` — human-readable description

```cue
myTest: {v: "ok"} @test(desc="description") @test(eq, {v: "ok"})
```

Purely a documentation annotation — does not affect the sub-test name or
produce any assertion.

---

## Empty placeholder and `CUE_UPDATE`

`@test()` (empty body) is a fill-in placeholder.  Running with `CUE_UPDATE=1`
evaluates the field and rewrites the attribute in the source file:

| Evaluated result | Rewritten attribute |
|------------------|---------------------|
| Value            | `@test(eq, <value>)` |
| Error            | `@test(err, code=<code>, contains="<msg>")` |

`CUE_UPDATE=diff` records a failing assertion as
`@test(..., skip:v3, diff="got …; want …")` rather than overwriting the
expected value.  `CUE_UPDATE=force` overwrites unconditionally.

---

## Version-specific overrides

Any directive may carry a `:vN` suffix to apply only under that evaluator
version.  When both a versioned and unversioned form appear on the same field,
the versioned form takes precedence:

```cue
// Uses 42 for all versions, but v3 overrides to 43.
myField: someExpr @test(eq, 42) @test(eq:v3, 43)

// Skip only under v3.
wip: someExpr @test(eq, 42) @test(skip:v3, why="known regression")
```

---

## Files in this directory

| File | Feature area |
|------|-------------|
| `basic.txtar` | Basic inline form: `eq`, `err`, `kind`, `closed`, `permute`, `ignore` |
| `directives.txtar` | Comprehensive directive coverage for the inline form |
| `struct_eq.txtar` | Struct-level `@test(eq, ...)` as decl attribute |
| `decl_eq.txtar` | File-level `@test(eq, ...)`: whole-file equality check |
| `decl_eq_pattern.txtar` | File-level `@test(eq, ...)` with pattern constraints |
| `decl_eq_mixed.txtar` | File-level `@test(eq, ...)` coexisting with field-level `@test` |

---

## Running the tests

```bash
# Run all inlinetest cases under the v3 evaluator
go test -run TestEvalV3 ./internal/core/adt

# Run a single sub-test
go test -run TestEvalV3/inlinetest/basic ./internal/core/adt

# Fill empty @test() placeholders
CUE_UPDATE=1 go test -run TestEvalV3/inlinetest ./internal/core/adt
```
