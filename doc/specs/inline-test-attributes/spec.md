# Inline Test Attributes

## Requirements

### Requirement: Test attribute syntax
CUE evaluator test files SHALL support `@test(...)` attributes on fields and inside structs (decl attributes) to express inline assertions. A field or struct MAY carry multiple `@test(...)` attributes; all of them MUST be satisfied for the test to pass.

#### Scenario: Field with single assertion
- **WHEN** a field is annotated `result: 42 @test(eq, 42)`
- **THEN** the test passes if the evaluated value of `result` equals `42`

#### Scenario: Field with multiple assertions
- **WHEN** a field is annotated `result: {a:1} @test(eq, {a:1}) @test(kind=struct)`
- **THEN** the test passes only if BOTH the equality check and the kind check are satisfied

---

### Requirement: Field attribute
When `@test(...)` appears as a *field attribute* (`field: value @test(...)`), the field value is the thing under test and the directive is an assertion applied directly to it. The test case name is the field name.

A txtar file is in inline-assertion mode when any of its CUE files contains at least one `@test(...)` attribute — as a field attribute, a decl attribute inside a struct at any nesting depth, or a file-scope decl attribute. All other txtar files use the existing golden-file mechanism.

#### Scenario: Field attribute triggers inline mode
- **WHEN** a txtar `.cue` file contains `result: 42 @test(eq, 42)` at the top level
- **THEN** the file is processed in inline-assertion mode and `result` is a test case

#### Scenario: No @test anywhere means golden-file mode
- **WHEN** a txtar `.cue` file contains no `@test(...)` attributes
- **THEN** the file is processed using the existing golden-file mechanism unchanged

---

### Requirement: Decl attribute
A `@test(...)` attribute may appear as a *decl attribute* — a bare attribute declaration inside a struct body at any nesting depth (provided the struct is on a concrete path) or at the top level of a `.cue` file (not as a field attribute). The value it is associated with depends on where it appears:

- **Inside a struct body** (`field: { @test(...) ... }`): the attribute is associated with the containing field's value. The struct may be at any nesting depth, as long as it is on a concrete path. This form is used by directives like `permute` (marks all sibling fields for permutation) and `eq`/`err` (asserts the containing field's value).
- **At file scope** (top-level `@test(...)` in a `.cue` file, outside any struct): the attribute is associated with the **entire file's** evaluated value. This is useful when pattern constraints (e.g. `{[X=string]: baz: X}`) contribute to the output and there is no single field to annotate.

Field-level `@test` attributes MAY coexist with decl attributes; all are checked independently.

```cue
// Struct-level decl: checks the value of "result"
result: {
    @test(eq, {a: 1})
    a: 1
}

// File-level decl: checks the entire file value
{[X=string]: baz: X}
bar: {}
@test(eq, {bar: baz: "bar"})
```

#### Scenario: Struct-level decl @test(eq) checks containing field value
- **WHEN** a field `result` has `@test(eq, {a: 1})` as a decl attribute inside its struct body
- **THEN** the runner compares the value of `result` against `{a: 1}`

#### Scenario: File-level @test(eq) checks entire file value
- **WHEN** a `.cue` file has `a: 1`, `b: a + 1`, and `@test(eq, {a: 1, b: 2})` at file scope
- **THEN** the runner compares the full file value against `{a: 1, b: 2}` and the test passes

#### Scenario: File-level and field-level @test coexist
- **WHEN** a `.cue` file has both a file-level `@test(eq, ...)` and a field carrying `@test(eq, ...)`
- **THEN** both assertions are checked independently

---

### Requirement: Directive with optional version suffix
The first positional argument of a `@test(...)` attribute SHALL be the *directive*, optionally followed by a colon and an evaluator version token (e.g., `v3`). A versioned directive SHALL apply only when the test runs under the matching evaluator version. When both a versioned and an unversioned instance of the same directive appear on the same field, the versioned form SHALL take precedence for its target version.

#### Scenario: Version-specific equality assertion
- **WHEN** a field carries `@test(eq, 1) @test(eq:v3, 2)` and the runner is evaluating with v3
- **THEN** the assertion `eq:v3` is used and the test checks that the value equals `2`

#### Scenario: Version-specific skip
- **WHEN** a test case carries `@test(skip:v3)` and the runner is evaluating with v3
- **THEN** the test is skipped for v3 only; it runs normally under other versions

---

### Requirement: `eq` directive
The `eq` directive SHALL assert equality between the evaluated value at the annotated field and an expected CUE expression. The comparison is performed by walking the expected expression as a parsed AST and comparing it structurally against the evaluated value — the expected expression is never compiled, which prevents evaluator bugs from masking mismatches.

Comparison behavior:
- Disjunctions are compared structurally, order-independently, preserving default markers (`*`).
- Pattern constraints (`[T]: V`) are compared as a guideline where possible; the runner checks for existence and value match on a best-effort basis.
- Definitions (`#D`) and hidden definitions (`_#D`) are compared.
- Concrete values (e.g. `1`, `"foo"`) use compile-and-compare semantics and match regardless of how the value was constructed.
- Structs: by default, field order is ignored. `@test(checkOrder)` as a decl attribute inside the expected struct additionally asserts that fields appear in the listed order.
- `@test(final)` as a decl attribute inside the expected struct opts into compile-and-compare for all fields in that struct (resolving disjunction defaults before comparison).
- `@test(final)` on a specific field inside the expected struct opts into compile-and-compare for that field only.

Error handling within the expected struct:
- A field inside the expected struct may carry `@test(err, ...)` to assert that the corresponding field in the evaluated value is an error instead of performing a normal value comparison.
- Positions in nested `@test(err)` specs use absolute line numbers since there is no meaningful anchor inside the attribute body.

Non-test attributes — both field attributes (e.g. `@json(...)`, `@protobuf(...)`) and decl attributes — inside the expected struct ARE compared: the runner SHALL verify that the corresponding location in the evaluated value carries exactly the same attributes (order-independent; duplicate keys supported). Both missing attributes (expected but absent in the evaluated value) and extra attributes (present in the evaluated value but not in the expected struct) are reported as failures. `@test(...)` attributes are always excluded from this comparison.

The expected value is supplied as a CUE expression argument: `@test(eq, <CUE-expr>)`. The argument is valid for any CUE expression that can appear on a single line; for complex multi-line values write them as a struct on one line or use nested attributes.

```cue
// Scalar
simple: 42 @test(eq, 42)

// Struct
obj: {a: 1, b: 2} @test(eq, {a: 1, b: 2})
```

#### Scenario: Inline scalar equality
- **WHEN** a field carries `@test(eq, 42)` and evaluates to `42`
- **THEN** the test passes

#### Scenario: Inline equality mismatch
- **WHEN** a field carries `@test(eq, 42)` and evaluates to `43`
- **THEN** the test fails with a message showing the expected and actual values

#### Scenario: Field attributes in expected struct are compared
- **WHEN** `@test(eq, {a: 1 @foo() @bar()})` is declared and the evaluated value has `a: 1 @bar() @foo()`
- **THEN** the test passes (attribute matching is order-independent)

#### Scenario: Missing attribute in evaluated value fails
- **WHEN** `@test(eq, {a: 1 @foo()})` is declared and the evaluated value has `a: 1` with no attributes
- **THEN** the test fails reporting the missing `@foo()` attribute

#### Scenario: Extra attribute in evaluated value fails
- **WHEN** `@test(eq, {a: 1})` is declared and the evaluated value has `a: 1 @foo()`
- **THEN** the test fails reporting the unexpected `@foo()` attribute

#### Scenario: Nested err directive asserts error on specific field
- **WHEN** `@test(eq, {a: _|_ @test(err, code=eval)})` is declared and field `a` in the evaluated value is an eval error
- **THEN** the test passes

#### Scenario: Error message rewording does not cause mismatch
- **WHEN** the evaluated value and the expected value have the same error kind and code but different message text
- **THEN** the test passes; only error presence, kind, and code are compared

#### Scenario: Error kind change causes mismatch
- **WHEN** the evaluated value has an `incomplete` error and the expected value has a `cycle` error
- **THEN** the test fails

#### Scenario: checkOrder asserts field ordering
- **WHEN** `@test(eq, {a: 1, b: 2, @test(checkOrder)})` is declared and the evaluated struct has fields in order `b`, `a`
- **THEN** the test fails reporting the field order mismatch

---

### Requirement: `leq` directive
The `leq` directive SHALL assert that the evaluated value is *subsumed by* the given CUE expression (i.e., the constraint subsumes the result — the result is at least as specific as the constraint). This allows asserting type-level constraints without pinning an exact value.

#### Scenario: Value subsumed by type constraint
- **WHEN** a field carries `@test(leq, int)` and evaluates to `42`
- **THEN** the test passes because `int` subsumes `42`

#### Scenario: Value not subsumed
- **WHEN** a field carries `@test(leq, string)` and evaluates to `42`
- **THEN** the test fails

---

### Requirement: `err` directive
The `err` directive SHALL assert that an error exists at the annotated location. Without sub-options, the error MUST exist at exactly the annotated field. The directive supports the following optional key-value sub-options:

- `code=<code>` — the error code must match. Valid codes include `cycle`, `eval`, `incomplete`, `structural`, `reference`, and any other code defined in `cuelang.org/go/cue/errors`.
- `contains="substring"` — the error message must contain the given substring.
- `any` (bare flag) — at least one **descendant** of the annotated field must have an error. The annotated field itself is not required to be an error. `code` MUST be specified when `any` is used.
- `pos=[spec ...]` — asserts the exact set of source positions reported by the error (as returned by `cuelang.org/go/cue/errors.Positions`). Each whitespace-delimited spec takes one of two forms:
  - `deltaLine:col` — position in the **same file** as the `@test` attribute, expressed as a signed line offset from an *anchor line* and a 1-indexed column. The anchor depends on where the `@test` attribute appears:
    - **Field attribute** (`field: value @test(...)`): anchor is the field's line in the stripped output (`deltaLine=0` = same line as the field).
    - **Struct-level decl attribute** (inside `{ @test(...) ... }`): anchor is the line of the opening `{` of the enclosing struct (`deltaLine=1` = first line inside the struct body). This keeps the assertion stable when the `@test` line itself is stripped, and gives the natural reading "N lines into the struct".
    - **File-level decl attribute** (`@test(...)` at file scope): anchor is the `@test` attribute's own line in the stripped output.
  - `filename:absLine:col` — position in a **different file** (e.g. a shared fixture), expressed as the archive file name, a 1-indexed absolute line number, and a 1-indexed column.
  An empty `pos=[]` is a fill-in placeholder: `CUE_UPDATE=1` fills in the actual positions. Non-empty `pos=[...]` that mismatches requires `CUE_UPDATE=force` to overwrite.

#### Scenario: Error at exact field
- **WHEN** a field carries `@test(err, code=cycle)` and evaluates to a cycle error at that field
- **THEN** the test passes

#### Scenario: Error in a descendant via any — field attribute form
- **WHEN** a field carries `@test(err, any, code=cycle)` as a *field attribute* and at least one descendant of that field has a cycle error
- **THEN** the test passes

#### Scenario: Error code mismatch
- **WHEN** a field carries `@test(err, code=cycle)` and the error code is `incomplete`
- **THEN** the test fails

#### Scenario: No error when error expected
- **WHEN** a field carries `@test(err)` and the field evaluates without error
- **THEN** the test fails

#### Scenario: pos= matches error positions on field attribute
- **WHEN** `@test(err, pos=[0:5 0:14])` is a **field attribute** and the error reports exactly two positions on the same line as the field (column 5 and column 14)
- **THEN** the test passes

#### Scenario: pos= matches error positions on struct decl attribute
- **WHEN** `d: { @test(err, pos=[1:5 2:2]) ... }` is declared and the error reports positions at the 1st and 2nd lines inside `d`'s `{` at the given columns
- **THEN** the test passes (`deltaLine=1` means 1 line after `{`, i.e. the first field)

#### Scenario: pos= empty placeholder fills on CUE_UPDATE
- **WHEN** `@test(err, pos=[])` is declared and `CUE_UPDATE=1` is set
- **THEN** the runner fills in the actual positions, writing e.g. `pos=[0:5 0:14]` to the source file

#### Scenario: pos= cross-file absolute position
- **WHEN** `@test(err, pos=[fixture.cue:3:5])` is declared and the error position is at absolute line 3, column 5 in `fixture.cue`
- **THEN** the test passes

#### Scenario: pos= mismatch fails test
- **WHEN** `@test(err, pos=[0:5])` is declared but the error has two positions
- **THEN** the test fails reporting the count mismatch

---

### Requirement: `kind` directive
The `kind` directive SHALL assert that the evaluated value is of the specified kind. Multiple kinds separated by `|` SHALL mean any of those kinds is acceptable. The kind is given as a key=value argument.

Valid kind names: `bool`, `int`, `float`, `string`, `bytes`, `struct`, `list`, `null`, `top` (or `_`), `bottom` (or `_|_`).

#### Scenario: Kind check passes
- **WHEN** a field carries `@test(kind=struct)` and evaluates to `{a: 1}`
- **THEN** the test passes

#### Scenario: Kind check fails
- **WHEN** a field carries `@test(kind=list)` and evaluates to `{a: 1}`
- **THEN** the test fails

#### Scenario: Multiple acceptable kinds
- **WHEN** a field carries `@test(kind=int|float)` and evaluates to `3.14`
- **THEN** the test passes because `float` is in the accepted set

---

### Requirement: `closed` directive
The `closed` directive (bare flag) SHALL assert that the evaluated struct is closed. `closed=false` SHALL assert that it is open.

#### Scenario: Closed struct assertion
- **WHEN** a field carries `@test(closed)` and the struct is closed
- **THEN** the test passes

#### Scenario: Open struct when closed required
- **WHEN** a field carries `@test(closed)` and the struct is open
- **THEN** the test fails

---

### Requirement: `skip` directive
The `skip` directive SHALL cause the test case or individual assertion to be skipped. An optional `why="reason"` key-value arg provides a human-readable explanation. A versioned form `skip:v3` skips only when running under evaluator version `v3`.

`@test(skip)` skips only the field or struct in which it is defined, not the entire file. Other test cases in the same file continue to run normally.

#### Scenario: Skip with reason
- **WHEN** a test case carries `@test(skip, why="not yet supported")`
- **THEN** the test is reported as skipped with the reason shown in the test output

#### Scenario: Skip is scoped to the annotated field
- **WHEN** field `a` carries `@test(skip)` and field `b` carries `@test(eq, 2)` in the same file
- **THEN** `a` is skipped but `b` runs normally

---

### Requirement: `todo` directive
The `todo` directive is like `skip`, but acts as a signal that the skipped test case is expected to be fixed in the near future rather than being permanently skipped. An optional `p=N` priority key-value arg classifies urgency: `p=0` is critical (must fix immediately), `p=1` is important (fix soon), `p=2` is good to have (fix when convenient). An optional `why="reason"` key-value arg provides a human-readable explanation.

`@test(todo)` skips only the field or struct in which it is defined, not the entire file.

#### Scenario: Todo skips with priority signal
- **WHEN** a test case carries `@test(todo, p=0, why="broken by recent evaluator change")`
- **THEN** the test is reported as skipped with the priority and reason shown in the test output

---

### Requirement: `desc` directive
The `desc` directive is a human-readable description annotation with no assertion semantics. It SHALL be silently accepted and ignored during test evaluation. Its only purpose is to document the intent of a test case in the source file.

```cue
result: 42 @test(eq, 42) @test(desc="the base case")
```

---

### Requirement: `permute` directive
The `permute` directive instructs the runner to generate all field-order permutations of a specified set of fields and assert that the evaluated result is the same for every permutation (modulo field ordering). This tests that the CUE evaluator is genuinely order-independent for the indicated fields.

The directive has two placement forms:

**Field attribute form** — `@test(permute)` on individual fields marks each field as a member of the permuted set. All fields carrying `@test(permute)` at the same struct level are permuted together as a group:

```cue
in: {
    a: 1     @test(permute)
    b: a+1   @test(permute)
    c: b+1   @test(permute)
}
```

**Decl attribute form** — `@test(permute)` inside a struct permutes *all* fields in that struct:

```cue
in: {
    @test(permute)  // permutes a, b, and c together
    a: 1, b: a+1, c: b+1
}
```

Both forms are equivalent when all fields at the level are marked. Use the field attribute form to exclude specific fields from permutation; use the decl form for brevity when all fields participate.

The runner SHALL generate all N! permutations of the marked fields, evaluate each, and assert the results are structurally identical (using AST comparison) modulo field ordering. If any permutation produces a different result, the test SHALL fail, listing the differing permutation.

#### Scenario: All permutations yield identical result
- **WHEN** `@test(permute)` is declared and all N! field-order permutations of the marked fields produce the same evaluated result
- **THEN** the test passes

#### Scenario: Permutation produces different result
- **WHEN** one permutation of the marked fields produces a different evaluated result from another
- **THEN** the test fails, reporting which permutation differs and how

#### Scenario: Decl form permutes all fields
- **WHEN** `@test(permute)` is placed as a decl attribute inside a struct containing three fields
- **THEN** the runner permutes all three fields (equivalent to placing `@test(permute)` on each individually)

#### Scenario: Field form selects subset
- **WHEN** only two of three fields carry `@test(permute)` as a field attribute
- **THEN** only those two fields are permuted; the third remains in its original position for all permutations

---

### Requirement: `permuteCount` directive
The `permuteCount` directive asserts the total number of permutations that were executed for the test case root. It is placed as a field attribute on the test root. When `CUE_UPDATE=1` is set, the count is auto-filled if empty or replaced if it differs.

```cue
in: {
    a: 1  @test(permute)
    b: a+1 @test(permute)
} @test(permuteCount, 2)
```

#### Scenario: Permutation count matches
- **WHEN** `@test(permuteCount, 6)` is declared and exactly 6 permutations ran
- **THEN** the test passes

#### Scenario: Permutation count mismatch
- **WHEN** `@test(permuteCount, 6)` is declared but only 2 permutations ran
- **THEN** the test fails reporting the count mismatch

---

### Requirement: `debugCheck` directive
The `debugCheck` directive asserts that the debug-printer output of the evaluated value matches the expected string. The debug-printer output is the same internal representation that appears in `out/eval` golden sections.

When `CUE_UPDATE=1` or `CUE_UPDATE=force` is set, the expected string is auto-filled or replaced. Unlike `eq`, a mismatch with `debugCheck` is a hard test failure (not a regression guard).

```cue
result: {a: 1} @test(debugCheck, """<debug output here>""")
```

An empty `@test(debugCheck)` with no argument behaves as a fill placeholder under `CUE_UPDATE=1`.

#### Scenario: Debug output matches
- **WHEN** a field carries `@test(debugCheck, "...")` and the debug-printer output equals the expected string
- **THEN** the test passes

#### Scenario: Debug output mismatch
- **WHEN** a field carries `@test(debugCheck, "...")` and the debug-printer output differs
- **THEN** the test fails

---

### Requirement: `debugOutput` directive
The `debugOutput` directive is an informational annotation that records the debug-printer output of the evaluated value. Unlike `debugCheck`, a mismatch does NOT fail the test — it only logs a difference and auto-updates when `CUE_UPDATE=1` is set.

This is useful for documenting what internal representation a value produces without locking the test to that exact representation.

```cue
result: {a: 1} @test(debugOutput, """<debug output here>""")
```

#### Scenario: Debug output annotation — no test failure on mismatch
- **WHEN** a field carries `@test(debugOutput, "...")` and the actual debug output differs
- **THEN** the difference is logged but the test does not fail

---

### Requirement: Empty `@test()` as placeholder
A field carrying `@test()` (empty attribute body) SHALL be treated as an unfilled assertion placeholder. When `CUE_UPDATE=1` or `CUE_UPDATE=force` is set, the runner SHALL evaluate the field and rewrite the attribute to `@test(eq, <actual_value>)`. Without `CUE_UPDATE`, an empty `@test()` SHALL cause the test to fail with a message prompting the author to run `CUE_UPDATE=1`.

#### Scenario: Scaffold assertion via CUE_UPDATE
- **WHEN** a field carries `@test()` and `CUE_UPDATE=1` is set
- **THEN** the txtar source file is updated with the evaluated value as an `@test(eq, ...)` assertion

---

### Requirement: Regression guard for failing `eq` assertions
When `CUE_UPDATE=1` is set and an `eq` assertion **fails** (genuine mismatch), the runner SHALL NOT silently overwrite the expected value. Instead it SHALL annotate the attribute with `skip:<version>` and `diff="..."` arguments that record the discrepancy without changing the nominal expected value. The test is then effectively skipped for that version.

`CUE_UPDATE=force` (`CUE_UPDATE=force` env value) SHALL overwrite the expected value unconditionally, regardless of whether it was passing before.

When a previously-failing (skip-annotated) assertion now passes again under `CUE_UPDATE=1`, the runner SHALL remove the stale `skip:` and `diff=` arguments, restoring the plain `@test(eq, <expr>)` form.

#### Scenario: Record failing assertion as regression guard
- **WHEN** `@test(eq, 42)` fails because the value is `43` and `CUE_UPDATE=1` is set
- **THEN** the attribute is rewritten to `@test(eq, 42, skip:v3, diff="got 43; want 42")` in the source file

#### Scenario: Remove stale skip on recovery
- **WHEN** `@test(eq, 42, skip:v3, diff="got 43; want 42")` now passes and `CUE_UPDATE=1` is set
- **THEN** the `skip:v3` and `diff=` arguments are removed, leaving `@test(eq, 42)`

#### Scenario: Force overwrite with CUE_UPDATE=force
- **WHEN** `@test(eq, 42)` fails because the value is `43` and `CUE_UPDATE=force` is set
- **THEN** the attribute is rewritten to `@test(eq, 43)` unconditionally

---

### Requirement: `out/errors.txt` documentary section
A txtar archive in inline-assertion mode MAY contain an `-- out/errors.txt --` section that records all evaluation errors (including incomplete errors) in a human-readable format with `[code]` prefixes. This section serves as documentation of expected error output — it is never auto-created and never causes test failures on its own.

The error format prefixes each error with its error code in square brackets, followed by the formatted error message from `cuelang.org/go/cue/errors`:

```
[eval] e: index out of range [4] with length 0:
    in.cue:4:11
[incomplete] f: undefined field: b:
    in.cue:5:11
```

Behavior matrix:

| Section present? | `CUE_UPDATE=1` | Normal run | `CUE_CHECK=1` |
|-----------------|---------------|------------|---------------|
| No              | skip (don't create) | skip | skip |
| Yes             | update silently | skip (no fail) | fail if stale |

Key points:
- The section is **never auto-created** by `CUE_UPDATE=1`. It must be added manually.
- A normal test run silently ignores differences in this section.
- `CUE_CHECK=1` enables strict mode: the section fails if its content is stale.
- `CUE_UPDATE=1` updates the section's content when it already exists.

Both child errors (incomplete, eval, cycle, etc.) and propagated-from-child error markers are included. Child-only marker errors (which don't carry their own message) are excluded.

#### Scenario: Section absent — always skipped
- **WHEN** the archive has no `-- out/errors.txt --` section
- **THEN** no errors.txt logic runs under any mode

#### Scenario: Section present — normal run ignores differences
- **WHEN** the archive has an `-- out/errors.txt --` section with stale content
- **THEN** the test passes (differences are silently ignored)

#### Scenario: Section present — CUE_UPDATE=1 updates content
- **WHEN** the archive has an `-- out/errors.txt --` section and `CUE_UPDATE=1` is set
- **THEN** the section is updated with the current error output

#### Scenario: Section present — CUE_CHECK=1 fails on stale content
- **WHEN** the archive has an `-- out/errors.txt --` section with stale content and `CUE_CHECK=1` is set
- **THEN** the test fails with a message showing the expected and actual error output
