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

#### Hidden fields in `eq` struct literals

When the expected value of an `@test(eq, {...})` contains a hidden field that
belongs to a named package, append `$<pkgname>` to the field name to select
the correct package scope:

```cue
// package foobar

#Spec: {
    _vars: {something: string}
    data: yaml.Marshal(_vars.something)
}

Val: #Spec & {
    _vars: something: "var-string"
} @test(eq, {
    _vars$foobar: something: "var-string"   // _vars scoped to package "foobar"
    data: "var-string\n"
})
```

Without the `$foobar` qualifier the comparison engine would look for an
anonymous hidden field `_vars` (i.e. `cue.Hid("_vars", "_")`).  With it,
the engine resolves `cue.Hid("_vars", "foobar")`, which matches the
`_vars(:foobar)` field produced by the CUE compiler for `package foobar`
sources.

The `$pkg` suffix is only meaningful in `@test(eq, {...})` expected-value
struct literals.  It is not valid CUE syntax in regular source files.

---

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
| `at=<path>` | navigate to sub-path before checking error (e.g. `at=a.b`) |
| `pos=[...]` | error positions must match (see below) |
| `args=[...]` | Msg() args must contain the listed values (see below) |
| `suberr=(...)` | sub-error spec for multi-error values (see below) |

#### `at=<path>` — assert error at sub-path

Navigates to the given CUE path (relative to the annotated field) before
checking for the error. Useful when the error occurs in a nested field that
cannot be directly annotated:

```cue
outer: {
    inner: bad: string & int
} @test(err, at=inner.bad, code=eval, contains="conflicting values")
```

The path must not include hidden fields (identifiers beginning with `_`); hidden
fields cannot be accessed via `cue.ParsePath` and are silently skipped by the
annotation infrastructure.

#### `pos=[...]` — error positions

Specifies expected error positions. Each position is written as `deltaLine:col`
relative to the `@test` attribute line, or `file:line:col` for positions in
other files. Positions are matched **order-independently** — the order of specs
does not need to match the order of actual positions. Commas between specs are
optional:

```cue
bad: x & y @test(err, code=eval, pos=[0:5, 0:9])
```

`pos=[]` is a fill-in placeholder. Running with `CUE_UPDATE=1` writes the
actual positions. Running with `CUE_UPDATE=force` overwrites existing
non-empty `pos=` specs too.

#### `args=[v1, v2, ...]` — Msg() argument check

Checks that the values returned by the error's `Msg()` method include
all listed strings (matched via `fmt.Sprint`, **order-independent**,
**subset check**):

```cue
e: [] & 4 @test(err, code=eval, contains="conflicting values", args=[list, int])
```

Design note: `args=` is a **subset check** — extra actual arguments not
listed in `args=` are allowed. This is intentional: `args=` is for
verifying the arguments that matter (e.g. type names) without having to
enumerate every argument. Arguments that happen to vary in order across
implementations can be checked without also repeating arguments already
covered by `contains=`.

If you need to verify the exact set of arguments, list all of them:

```cue
e: [3][-1] @test(err, code=eval, args=[-1])   // only one arg — effectively exact
```

#### `suberr=(...)` — sub-error specs

For errors composed of multiple sub-errors (typically failed disjunctions),
each `suberr=(...)` spec matches one sub-error **order-independently**.
The body accepts the same options as `@test(err, ...)`:

```cue
x: null | {n: 3}
x: #empty & {n: 3} @test(err, code=eval,
    suberr=(contains="conflicting values", args=[struct, null]),
    suberr=(contains="not allowed"))
```

Matching is two-pass:
1. Specs with non-empty `pos=` are matched first (position is a stronger
   discriminator than `contains=`).
2. Remaining specs are matched by `contains=` against unmatched actual
   sub-errors.

`pos=[]` placeholders inside `suberr=(...)` trigger write-back on
`CUE_UPDATE=1`. All position updates for the same `@test` attribute are
applied atomically.

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

### `pass` — value is not an error

Checks that the value has no error (i.e. `val.Err() == nil`).

```cue
ok:  42       @test(pass)
ok2: {a: 1}   @test(pass)
```

### `allows` — field allowance

Checks whether `val.Allows(sel)` returns the expected result for the given
selector expression. Valid on struct and list values.

```cue
openStruct:   {a: 1}        @test(allows, b)          // open: any field allowed
knownField:   close({a: 1}) @test(allows, a)           // closed: known field allowed
unknownField: close({a: 1}) @test(allows=false, b)     // closed: unknown field denied
anyPattern:   {[string]: 1} @test(allows, [string])    // any-string pattern allowed
openList:     [...]          @test(allows, [int])       // open list: int index allowed
```

| Selector form | Meaning |
|---------------|---------|
| `foo` or `"foo"` | Regular string field |
| `#Def` | Definition field |
| `[string]` | Any-string pattern (`cue.AnyString`) |
| `[int]` | Any-index pattern (`cue.AnyIndex`) |

### `debugCheck` — debug-printer output

```cue
debugScalar: 42    @test(debugCheck, "(int){ 42 }")
debugStruct: {a: 1} @test(debugCheck, "(struct){\n  a: (int){ 1 }\n}")
```

Compares the string output of `internal/core/debug`'s printer applied to the
evaluated value.  Useful for verifying internal representation details that `eq`
does not capture.

### `debug` — informational debug-printer annotation

```cue
result: {a: 1} @test(debug, "(struct){\n  a: (int){ 1 }\n}")
```

Records the debug-printer output as an informational annotation.  Unlike
`debugCheck`, a mismatch does **not** fail the test — it only logs and
auto-updates when `CUE_UPDATE=1` is set.  Useful for documenting what
internal representation a value produces without locking the test to it.

### `skip` — skip a test case

```cue
wip: someExpr @test(skip, why="not yet implemented")
```

A versioned form `skip:v3` skips only under evaluator version `v3`.

### `permute` — field-order independence

Asserts that the marked fields produce the same result in all N! orderings.
The runner logs the number of permutations evaluated for each group via
`t.Logf`.

Two placement forms:

```cue
// Field attribute form: mark each field to include in the permutation set.
permuteStruct: {
    x: y + 1 @test(permute)
    y: 2     @test(permute)
    @test(eq, {x: 3, y: 2})
    @test(permuteCount, 2)
}

// Decl attribute form: permute all fields in the struct.
permuteStruct: {
    @test(permute)
    a: b + c
    b: 1
    c: 2
    @test(eq, {a: 3, b: 1, c: 2})
    @test(permuteCount, 6)
}
```

### `permuteCount` — verify permutation count

```cue
permuteStruct: {
    a: b + c @test(permute)
    b: 1     @test(permute)
    c: 2     @test(permute)
    @test(eq, {a: 3, b: 1, c: 2})
    @test(permuteCount, 6)
}
```

Placed alongside `@test(permute)` in the same struct, asserts the count for
that permutation group.  May also be placed at the test-root level to assert
the **total** count across all groups within the root:

```cue
concretePermute: {
    x: { @test(permute); alpha: 1, beta: 2, gamma: 3 }
    y: { @test(permute); alpha: 1, beta: 2, gamma: 3 }
    @test(eq, {x: {alpha: 1, beta: 2, gamma: 3}, y: {alpha: 1, beta: 2, gamma: 3}})
    @test(permuteCount, 12) // 2 structs × 3! = 12
}
```

`@test(permuteCount)` (no argument) is a fill-in placeholder.  Auto-updated
by `CUE_UPDATE=1`; `CUE_UPDATE=force` overwrites an existing non-empty count.

### `todo` — expected-to-fail wrapper

```cue
result: broken @test(eq, 42) @test(todo, p=1, why="evaluator bug #123")
```

Marks the annotated field as expected-to-fail: all directives still run, but
failures are suppressed (logged, not reported as errors).  If all directives
pass, the runner emits a WARNING that `@test(todo)` may no longer be needed.

### `eq:todo` — expected future value

```cue
v: gotWrong @test(eq, gotWrong) @test(eq:todo, expectedRight)
```

Documents what the field *should* evaluate to once a known issue is fixed.  A
mismatch is not a test failure (logged as "still failing"); a match emits a
WARNING suggesting the annotation be promoted to `@test(eq, ...)`.

### `err:todo` — expected future error

```cue
x: 42 @test(eq, 42, incorrect) @test(err:todo, p=1, code=eval)
```

Like `eq:todo` but for error assertions.  A mismatch (field does not yet
produce the expected error) is logged as "still failing", not a failure.  A
match emits a WARNING to upgrade the annotation.

### `incorrect` — document known-incorrect behavior

```cue
x: 42 @test(eq, 42, incorrect)
x: 1/0 @test(err, code=eval, incorrect)
```

Applicable to any assertion directive.  Marks the assertion as documenting the
evaluator's *current* output even though that output is known to be wrong.

- Passes (documented wrong value still present): logs `NOTE: ... matches (documented as known incorrect behavior)`. No test failure.
- Fails (behavior has changed): **test fails** — any change needs attention (may be a fix or a new regression).

Typical pattern: pair with `err:todo` to record both the current wrong value
and the expected correct behavior:

```cue
x: 42 @test(eq, 42, incorrect) @test(err:todo, p=1, code=eval)
```

### `p=N` — fix priority

```cue
x: 42 @test(eq:todo, 99, p=1)
x: 1/0 @test(err:todo, p=0, code=eval)
result: bad @test(eq, bad) @test(todo, p=2, why="cleanup")
```

Attaches a numeric priority to any `:todo` directive.  `p=0` is critical,
`p=1` important, `p=2` good-to-have.  Purely informational — shown in log
output; does not affect pass/fail behavior.

### `desc` — human-readable description

```cue
myTest: {v: "ok"} @test(desc="description") @test(eq, {v: "ok"})
```

Purely a documentation annotation — does not affect the sub-test name or
produce any assertion.

---

## `shareID` — vertex sharing assertion

`@test(shareID=name)` asserts that all fields annotated with the same
`name` share the same underlying `*adt.Vertex` (pointer identity after
following indirections):

```cue
a: {x: 1}
b: a
@test(eq, {
    a: {x: 1} @test(shareID=A)   // first occurrence: eq check runs normally
    b: a       @test(shareID=A)   // subsequent occurrences: eq check skipped
})
```

Rules:
- The **first** field with a given `shareID` name has its eq check run
  normally (so every value-expr pair is validated at least once).
- **Subsequent** fields with the same name skip the eq check — their
  expression is treated as documentation only.
- The sharing assertion itself runs after all eq checks.

`@test(shareID=name)` may only appear inside a `@test(eq, {...})` body.

---

## Empty placeholder and `CUE_UPDATE`

`@test()` (empty body) is a fill-in placeholder.  Running with `CUE_UPDATE=1`
evaluates the field and rewrites the attribute in the source file:

| Evaluated result | Rewritten attribute |
|------------------|---------------------|
| Value            | `@test(eq, <value>)` |
| Error            | `@test(err, code=<code>, contains="<msg>")` |

`CUE_UPDATE=diff` shows a unified diff of what `CUE_UPDATE=1` *would* write,
without modifying any files.  Documentary sections (e.g. `out/errors.txt`)
are also validated in this mode.  `CUE_UPDATE=force` overwrites unconditionally,
including non-empty `pos=` specs that would normally require manual review.

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
