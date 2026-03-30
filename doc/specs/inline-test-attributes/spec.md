# Inline Test Attributes

## Requirements

### Requirement: Test attribute syntax
CUE evaluator test files SHALL support `@test(...)` attributes on fields and inside structs (decl attributes) to express inline assertions. A field or struct MAY carry multiple `@test(...)` attributes; all of them MUST be satisfied for the test to pass.

#### Scenario: Field with single assertion
- **WHEN** a field is annotated `result: 42 @test(eq, 42)`
- **THEN** the test passes if the evaluated value of `result` equals `42`

#### Scenario: Field with multiple assertions
- **WHEN** a field is annotated `result: {a:1} @test(eq, "{a:1}") @test(kind=struct)`
- **THEN** the test passes only if BOTH the equality check and the kind check are satisfied

---

### Requirement: Inline form — field attribute on the value under test
When `@test(...)` appears as a *field attribute* (`field: value @test(...)`), the field value is the thing under test and the directive is an assertion applied directly to it. The test case name is the field name. This form is used for simple, point-in-time assertions.

A txtar file is in inline-assertion mode when any of its CUE files contains at least one `@test(...)` attribute — as a field attribute, a decl attribute inside a struct, or a file-level decl attribute. All other txtar files use the existing golden-file mechanism.

#### Scenario: Inline field attribute triggers inline mode
- **WHEN** a txtar `.cue` file contains `result: 42 @test(eq, 42)` at the top level
- **THEN** the file is processed in inline-assertion mode and `result` is a test case

#### Scenario: No @test anywhere means golden-file mode
- **WHEN** a txtar `.cue` file contains no `@test(...)` attributes
- **THEN** the file is processed using the existing golden-file mechanism unchanged

---

#### Scenario: leq sub-field checked by subsumption
- **WHEN** a container has `in` and `leq` sub-fields
- **THEN** the runner asserts that `evaluate(in)` is subsumed by `evaluate(leq)`

#### Scenario: eq and leq both present
- **WHEN** a container has both `eq` and `leq`
- **THEN** both checks run independently; both must pass

#### Scenario: Error assertion as container decl attr
- **WHEN** a container carries `@test(err, code=cycle, any)` and a descendant of `evaluate(in)` has a cycle error
- **THEN** the test passes; no `eq` field is required

#### Scenario: Error assertion on eq sub-field
- **WHEN** `eq: { a: _ @test(err, code=cycle) }` and field `a` in `evaluate(in)` has a cycle error
- **THEN** the test passes for that field

#### Scenario: Version-specific result
- **WHEN** a container has `eq: { result: 1 }` and `v3: { eq: { result: 2 } }` and the runner is using v3
- **THEN** `v3.eq` is used for the equality check instead of the top-level `eq`

---

### Requirement: File-level form — decl `@test(...)` at file scope
A `@test(eq, VALUE)` decl attribute appearing at the top level of a `.cue` file (not inside a struct field) SHALL check the **entire file's** evaluated value against `VALUE`. Field-level `@test` attributes MAY still coexist with a file-level `@test`; both SHALL be checked independently.

This form is particularly useful when pattern constraints (e.g. `{[X=string]: baz: X}`) contribute to the output.

```cue
// File-level @test checks the whole file
{[X=string]: baz: X}
bar: {}
@test(eq, {bar: baz: "bar"})
```

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
The `eq` directive SHALL assert equality between the evaluated value at the annotated field and an expected value, using `internal/core/diff` on the two `*adt.Vertex` trees. The diff SHALL ignore error message text (exact wording), so that rewording of error messages does not cause test failures; error presence, kind, and code SHALL still be part of the comparison. Insignificant syntactic differences such as let-expression substitution, field ordering, and comment presence SHALL NOT cause inequality. Content for the `eq` field or attribute argument SHALL be generated using `internal/core/export` + `cue/format` when `CUE_UPDATE=1` fills a placeholder.

Non-test field attributes (e.g. `@json(...)`, `@protobuf(...)`) on fields inside the expected struct ARE compared: each field in the expected struct may carry non-`@test` attributes, and the runner SHALL verify that the corresponding field in the evaluated value carries the same attributes (order-independent; duplicate keys supported). Missing or unexpected attributes on the evaluated field are reported as failures. `@test(...)` attributes are always excluded from this comparison as they are test harness metadata.

Fields inside the expected struct MAY carry `@test(err, ...)` to assert that the corresponding field in the evaluated value is an error instead of performing a normal value comparison. Positions in nested `@test(err)` specs use absolute line numbers (not deltas) since there is no meaningful anchor line inside the attribute body.

The expected value is supplied in one of two forms:

- **Inline expression**: `@test(eq, <CUE-expr>)` — the argument is a CUE expression, valid for simple single-line values.
- **`file=` form**: see the `file` directive requirement; the expected value is in a named txtar section, suitable for multi-line values.

```cue
// Inline form
simple: 42 @test(eq, 42)
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

#### Scenario: Nested err directive asserts error on specific field
- **WHEN** `@test(eq, {a: _|_ @test(err, code=eval)})` is declared and field `a` in the evaluated value is an eval error
- **THEN** the test passes

#### Scenario: Let-expression substitution does not cause mismatch
- **WHEN** the evaluated value uses let-binding internally but the result is structurally equal to the expected value
- **THEN** the test passes

#### Scenario: Error message rewording does not cause mismatch
- **WHEN** the evaluated value and the expected value have the same error kind and code but different message text
- **THEN** the test passes; only error presence, kind, and code are compared

#### Scenario: Error kind change causes mismatch
- **WHEN** the evaluated value has an `incomplete` error and the expected value has a `cycle` error
- **THEN** the test fails

---

### Requirement: `leq` directive
The `leq` directive SHALL assert that the evaluated value is *subsumed by* the given CUE expression (i.e., the result is at least as specific as the constraint). This allows asserting type-level constraints without pinning an exact value.

#### Scenario: Value subsumed by type constraint
- **WHEN** a field carries `@test(leq, int)` and evaluates to `42`
- **THEN** the test passes because `42 ⊑ int`

#### Scenario: Value not subsumed
- **WHEN** a field carries `@test(leq, string)` and evaluates to `42`
- **THEN** the test fails

---

### Requirement: `err` directive
The `err` directive SHALL assert that an error exists at the annotated location. Without sub-options, the error MUST exist at exactly the annotated field. The directive supports the following optional key-value sub-options:

- `code=<code>` — the error code must match. Valid codes include `cycle`, `eval`, `incomplete`, `structural`, `reference`, and any other code defined in `cuelang.org/go/cue/errors`.
- `contains="substring"` — the error message must contain the given substring.
- `severity=error|warning|…` — the error must have the given severity.
- `any` (bare flag) — at least one **descendant** of the annotated field must have an error. The annotated field itself is not required to be an error. `code` MUST be specified when `any` is used.
- `path=(p1|p2|…)` — the error must exist at at least one of the listed paths, expressed relative to the test-case root. Mutually exclusive with placing the attribute on a specific field and with `any`.
- `pos=[spec ...]` — asserts the exact set of source positions reported by the error (as returned by `cuelang.org/go/cue/errors.Positions`). Each whitespace-delimited spec takes one of two forms:
  - `deltaLine:col` — position in the **same file** as the `@test` attribute, expressed as a signed line offset from an *anchor line* and a 1-indexed column. The anchor depends on where the `@test` attribute appears:
    - **Field attribute** (`field: value @test(...)`): anchor is the field's line in the stripped output (`deltaLine=0` = same line as the field).
    - **Struct-level decl attribute** (inside `{ @test(...) ... }`): anchor is the line of the opening `{` of the enclosing struct (`deltaLine=1` = first line inside the struct body). This keeps the assertion stable when the `@test` line itself is stripped, and gives the natural reading "N lines into the struct".
    - **File-level decl attribute** (`@test(...)` at file scope): anchor is the `@test` attribute's own line in the stripped output (`deltaLine=0` = the line immediately after the stripped attribute, i.e. the first file-scope declaration).
  - `filename:absLine:col` — position in a **different file** (e.g. a shared fixture), expressed as the archive file name, a 1-indexed absolute line number, and a 1-indexed column.
  An empty `pos=[]` is a fill-in placeholder: `CUE_UPDATE=1` fills in the actual positions. Non-empty `pos=[...]` that mismatches requires `CUE_UPDATE=force` to overwrite.

#### Scenario: Error at exact field
- **WHEN** a field carries `@test(err, code=cycle)` and evaluates to a cycle error at that field
- **THEN** the test passes

#### Scenario: Error at either of two fields
- **WHEN** the test-case root carries `@test(err, path=(a|b), code=cycle)` and a cycle error exists at field `a`
- **THEN** the test passes because the error exists at one of the listed paths

#### Scenario: Error in a descendant via any — inline form
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

### Requirement: `ignore` directive
The `ignore` directive SHALL prevent a top-level field from being run as a sub-test. This is intended for shared reference fields or helpers that live in the same `.cue` file as test cases but are not themselves test cases.

`@test(ignore)` may be placed as either a **field attribute** or a **decl attribute** (inside the struct body of the field). Both forms have identical semantics.

```cue
// Field-attribute form: @test(ignore) on the field itself.
fixture: {
    base: 42
    multiplier: 3
} @test(ignore)

// Decl-attribute form: @test(ignore) inside the struct body.
helper: {
    @test(ignore)
    value: 10
}

result: fixture.base * fixture.multiplier @test(eq, 126)
usesHelper: helper.value + 5 @test(eq, 15)
```

A file with **no `@test` attributes at all** is treated as a pure fixture file: its fields are compiled into the value but no sub-tests are registered for them. No `@test(ignore)` annotation is required.

#### Scenario: @test(ignore) field attribute suppresses test execution
- **WHEN** a top-level field `fixture` carries `@test(ignore)` as a field attribute
- **THEN** no sub-test is registered for `fixture`

#### Scenario: @test(ignore) decl attribute suppresses test execution
- **WHEN** a top-level field `helper` carries `@test(ignore)` as a decl attribute inside its struct body
- **THEN** no sub-test is registered for `helper`

#### Scenario: @test(ignore) field is not run as a sub-test
- **WHEN** a field carries `@test(ignore)` (either form)
- **THEN** no `t.Run` is called for that field; it does not appear in test output

#### Scenario: Fixture file still compiled into evaluated value
- **WHEN** a pure fixture file defines `shared: base: 42` and a test file references `shared.base`
- **THEN** the reference resolves correctly; the fixture file's fields are part of the evaluated CUE value

---

### Requirement: `kind` directive
The `kind` directive SHALL assert that the evaluated value is of the specified kind. Multiple kinds separated by `|` SHALL mean any of those kinds is acceptable.

#### Scenario: Kind check passes
- **WHEN** a field carries `@test(kind=struct)` and evaluates to `{a: 1}`
- **THEN** the test passes

#### Scenario: Kind check fails
- **WHEN** a field carries `@test(kind=list)` and evaluates to `{a: 1}`
- **THEN** the test fails

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

#### Scenario: Skip with reason
- **WHEN** a test case carries `@test(skip, why="not yet supported")`
- **THEN** the test is reported as skipped with the reason shown in the test output

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

The runner SHALL generate all N! permutations of the marked fields, evaluate each, and assert the results are structurally identical (using `internal/core/diff`) modulo field ordering. If any permutation produces a different result, the test SHALL fail, listing the differing permutation.

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

### Requirement: Empty `@test()` as placeholder
A field carrying `@test()` (empty attribute body) SHALL be treated as an unfilled assertion placeholder. When `CUE_UPDATE=1` or `CUE_UPDATE=force` is set, the runner SHALL evaluate the field and rewrite the attribute to `@test(eq, <actual_value>)`. If the field evaluates to an error, the rewritten form SHALL be `@test(err, code=<code>, contains="<message>")`. Without `CUE_UPDATE`, an empty `@test()` SHALL cause the test to fail with a message prompting the author to run `CUE_UPDATE=1`.

#### Scenario: Scaffold assertion via CUE_UPDATE
- **WHEN** a field carries `@test()` and `CUE_UPDATE=1` is set
- **THEN** the txtar source file is updated with the evaluated value as an `@test(eq, ...)` assertion

---

### Requirement: `CUE_UPDATE=diff` mode
When `CUE_UPDATE=diff` is set and a non-skipped assertion fails, the runner SHALL rewrite the attribute to add `skip:<version>` and `diff="<description>"` arguments capturing the discrepancy, without changing the nominal assertion. The `diff=` value is informational only and SHALL be ignored during assertion evaluation.

When `CUE_UPDATE=1` is set and an assertion that was previously passing now fails, the runner SHALL behave as `CUE_UPDATE=diff` for that assertion (recording the failure) rather than silently overwriting the expected value. `CUE_UPDATE=force` SHALL overwrite the expected value unconditionally.

#### Scenario: Record failing assertion inline
- **WHEN** `@test(eq, 42)` fails because the value is `43` and `CUE_UPDATE=diff` is set
- **THEN** the attribute is rewritten to `@test(eq, 42, skip:v3, diff="got 43; want 42")` in the source file

#### Scenario: Remove stale skip on recovery
- **WHEN** `@test(eq, 42, skip:v3, diff="got 43; want 42")` now passes and `CUE_UPDATE=1` is set
- **THEN** the `skip:v3` and `diff=` arguments are removed, leaving `@test(eq, 42)`

---

### Requirement: `file` directive for expected values in named txtar sections
The `file` directive SHALL assert that the evaluated value at the annotated field equals the CUE expression stored in the named txtar section. It uses the same `internal/core/diff` comparison as `eq` (error message text ignored), and reads the expected value from a named txtar section rather than an attribute argument, allowing multi-line and complex expected values.

The section name is a free-form txtar filename; by convention it lives under `expected/` to distinguish it from `out/` golden sections and CUE source files. Comparison uses the same semantics as `eq`, not textual.

`CUE_UPDATE=1` SHALL create or update the named section with the actual evaluated value, subject to the same regression-guard rules as all other inline rewrites. A versioned form `@test(file:v3, "name.cue")` stores the version-specific expected value in a separate named section.

```cue
complexResult: { ... } @test(file, "expected/complexResult.cue")
```

```
-- expected/complexResult.cue --
{
    a: 1
    b: "hello"
    nested: {x: [1, 2, 3]}
}
```

#### Scenario: File comparison passes
- **WHEN** `@test(file, "expected/r.cue")` is declared and the evaluated value equals the CUE expression in that section
- **THEN** the test passes

#### Scenario: CUE_UPDATE fills empty section
- **WHEN** `@test(file, "expected/r.cue")` is declared, `CUE_UPDATE=1` is set, and the section does not yet exist
- **THEN** the section is created with the actual evaluated value

#### Scenario: Versioned expected file
- **WHEN** a field carries `@test(file, "expected/r.cue") @test(file:v3, "expected/r-v3.cue")` and the runner is using v3
- **THEN** comparison uses `expected/r-v3.cue`; under other versions `expected/r.cue` is used

---

