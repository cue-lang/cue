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

### Requirement: `guidance=` universal flag
Any `@test(...)` directive that can produce a test failure MAY carry a `guidance="..."` key-value flag. When an assertion fails, the runner logs the guidance string as an additional note after the failure message. This is intended to provide context for automated tools (such as AI assistants) that inspect test failures, and for human readers who need background on why the expected value was chosen.

`guidance=` is purely informational — it has no effect on whether a test passes or fails. It does not modify any comparison or matching logic. It is silently accepted by all directives that report failures (`eq`, `leq`, `err`, `kind`, `closed`, `debugCheck`).

```cue
a: c: 1 @test(err, code=eval,
    contains="field not allowed",
    guidance="v3 only reports the direct definition position; see out/todo.txt")
```

When an AI tool encounters a test failure on a field that carries `guidance="..."`, it SHOULD read the guidance text before attempting to diagnose or fix the failure. The guidance may explain known evaluator differences, link to a tracking issue, or describe why the expected value is correct despite appearances.

#### Scenario: guidance= is logged on failure
- **WHEN** `@test(eq, 42, guidance="check the evaluator cycle")` is declared and the field evaluates to `43`
- **THEN** the test fails AND the runner logs `hint: check the evaluator cycle` immediately after the failure message

#### Scenario: guidance= has no effect on passing test
- **WHEN** `@test(eq, 42, guidance="...")` is declared and the field evaluates to `42`
- **THEN** the test passes; the guidance string is not logged

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
- Let bindings (`let x = expr`) in the expected struct SHALL be compared: the runner checks that the corresponding let binding in the evaluated vertex has the same value as `expr`.

Skipping sub-comparisons — `@test(ignore)` inside an expected value:
- A field inside the expected struct MAY carry `@test(ignore)` as a field attribute. When present, `eq`'s recursive descent into that element is halted: the element need not be present in the evaluated value and its value is not compared.
- `@test(ignore)` does NOT suppress other directives on the same element. For example, `b: _ @test(ignore) @test(err, any)` will still run the `@test(err, any)` check on `b`'s subtree; only the `eq` value comparison is skipped.
- To skip a let binding's value comparison, omit the let clause from the expected struct entirely — the runner only checks lets that are explicitly listed.

```cue
// Skip a subfield but still assert it contains an error somewhere:
result @test(eq, {
    a: 1
    b: _ @test(ignore) @test(err, any, code=cycle)  // skip eq check; still check for cycle error in b
})

// To skip checking a let binding, simply omit it from the expected struct:
result @test(eq, {
    a: 1
    // let b is not listed, so its value is not checked
    c: 3
})
```

Error handling within the expected struct:
- A field inside the expected struct may carry `@test(err, ...)` to assert that the corresponding field in the evaluated value is an error instead of performing a normal value comparison.
- Positions in nested `@test(err)` specs use absolute line numbers since there is no meaningful anchor inside the attribute body.

Non-test attributes — both field attributes (e.g. `@json(...)`, `@protobuf(...)`) and decl attributes — inside the expected struct ARE compared: the runner SHALL verify that the corresponding location in the evaluated value carries exactly the same attributes (order-independent; duplicate keys supported). Both missing attributes (expected but absent in the evaluated value) and extra attributes (present in the evaluated value but not in the expected struct) are reported as failures. `@test(...)` attributes are always excluded from this comparison.

Hidden fields in package-scoped sources — `$pkg` qualifier:
When the evaluated value contains a hidden field that belongs to a named CUE package (e.g. `_vars` in `package foobar` compiles to `_vars(:foobar)` internally), the expected struct in `@test(eq, {...})` must qualify the hidden field name with `$<pkgname>` to select the correct package scope. Without the qualifier the runner looks for an anonymous hidden field (`cue.Hid(name, "_")`); with it the runner resolves `cue.Hid(name, pkgname)`:

```cue
// package foobar

Val: #Spec & {
    _vars: something: "var-string"
} @test(eq, {
    _vars$foobar: something: "var-string"   // hidden field _vars scoped to package "foobar"
    data: "var-string\n"
})
```

The `$pkg` suffix is only meaningful inside `@test(eq, {...})` expected-value struct literals. It is not valid CUE syntax in regular source files.

The expected value is supplied as a CUE expression argument: `@test(eq, <CUE-expr>)`. The argument is valid for any CUE expression that can appear on a single line; for complex multi-line values write them as a struct on one line or use nested attributes.

```cue
// Scalar
simple: 42 @test(eq, 42)

// Struct
obj: {a: 1, b: 2} @test(eq, {a: 1, b: 2})
```

#### Scenario: Package-scoped hidden field with $pkg qualifier
- **WHEN** `@test(eq, {_vars$foobar: something: "val"})` is declared and the evaluated value has hidden field `_vars(:foobar)` with `something: "val"`
- **THEN** the test passes; the `$foobar` qualifier directs the runner to use `cue.Hid("_vars", "foobar")` (with `:foobar` fallback for inline-compiled sources)

#### Scenario: Package-scoped hidden field without qualifier fails
- **WHEN** `@test(eq, {_vars: something: "val"})` is declared and the only hidden field is package-scoped `_vars(:foobar)` (not anonymous)
- **THEN** the test fails because `cue.Hid("_vars", "_")` does not find the package-scoped field

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

#### Scenario: Let binding is compared
- **WHEN** `@test(eq, {a: 1, let b = 3})` is declared and the evaluated vertex has `let b = 3`
- **THEN** the test passes

#### Scenario: Let binding value mismatch fails
- **WHEN** `@test(eq, {let b = 3})` is declared but the evaluated vertex has `let b = 4`
- **THEN** the test fails reporting the let value mismatch

#### Scenario: @test(ignore) skips subfield eq check
- **WHEN** `@test(eq, {a: 1, b: _ @test(ignore)})` is declared and the evaluated struct has `a: 1` with any value for `b` (or no `b` at all)
- **THEN** the `eq` comparison of `b` is skipped; the test passes

#### Scenario: @test(ignore) does not suppress other directives
- **WHEN** `@test(eq, {b: _ @test(ignore) @test(err, any, code=cycle)})` is declared and `b` in the evaluated value contains a cycle error somewhere in its subtree
- **THEN** the `eq` check on `b` is skipped, but the `@test(err, any, code=cycle)` assertion still runs and passes

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
- `at=<path>` — navigate to the given CUE path (relative to the annotated field) before checking the error. Useful when the erroneous sub-field cannot be directly annotated (e.g. it is inside a comprehension or a pattern constraint). `pos=` is not compatible with `any`; `at=` and `any` may not be combined.
- `args=[v1, v2, ...]` — the values returned by the error's `Msg()` method must include all listed strings (matched via `fmt.Sprint`, order-independent, subset check). See `Requirement: err directive — args= sub-option` for details.
- `suberr=(...)` — matches one sub-error of a multi-error value (e.g. a failed disjunction). Multiple `suberr=` entries match sub-errors order-independently. The body accepts the same sub-options as `@test(err, ...)`. See `Requirement: err directive — suberr=(...)` for details.
- `pos=[spec, ...]` — asserts the set of source positions reported by the error (as returned by `cuelang.org/go/cue/errors.Positions`). Matching is **order-independent**: the order of specs need not match the order of actual positions. **Commas between specs are required.** Each spec takes one of two forms:
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

#### Scenario: at= navigates to nested error
- **WHEN** `outer: { inner: bad: string & int } @test(err, at=inner.bad, code=eval)` is declared
- **THEN** the runner navigates to `outer.inner.bad`, finds an eval error there, and the test passes

#### Scenario: at= sub-path not found fails
- **WHEN** `x: {a: 1} @test(err, at=a.nonexistent)` is declared
- **THEN** the test fails reporting that the sub-path was not found

#### Scenario: Error code mismatch
- **WHEN** a field carries `@test(err, code=cycle)` and the error code is `incomplete`
- **THEN** the test fails

#### Scenario: No error when error expected
- **WHEN** a field carries `@test(err)` and the field evaluates without error
- **THEN** the test fails

#### Scenario: pos= matches error positions on field attribute
- **WHEN** `@test(err, pos=[0:5, 0:14])` is a **field attribute** and the error reports exactly two positions on the same line as the field (column 5 and column 14)
- **THEN** the test passes (order of specs need not match order of actual positions)

#### Scenario: pos= matches error positions on struct decl attribute
- **WHEN** `d: { @test(err, pos=[1:5, 2:2]) ... }` is declared and the error reports positions at the 1st and 2nd lines inside `d`'s `{` at the given columns
- **THEN** the test passes (`deltaLine=1` means 1 line after `{`, i.e. the first field)

#### Scenario: pos= is order-independent
- **WHEN** `@test(err, pos=[0:14, 0:5])` is declared and the error reports positions at columns 5 and 14 (in that order)
- **THEN** the test passes regardless of the order specs are listed

#### Scenario: pos= empty placeholder fills on CUE_UPDATE
- **WHEN** `@test(err, pos=[])` is declared and `CUE_UPDATE=1` is set
- **THEN** the runner fills in the actual positions, writing e.g. `pos=[0:5, 0:14]` to the source file

#### Scenario: pos= cross-file absolute position
- **WHEN** `@test(err, pos=[fixture.cue:3:5])` is declared and the error position is at absolute line 3, column 5 in `fixture.cue`
- **THEN** the test passes

#### Scenario: pos= mismatch fails test
- **WHEN** `@test(err, pos=[0:5])` is declared but the error has two positions
- **THEN** the test fails reporting the count mismatch

---

### Requirement: `err` directive — `args=` sub-option
The `args=[v1, v2, ...]` sub-option of `@test(err, ...)` asserts that the error's `Msg()` method returns arguments that include all listed strings. The check is **order-independent** and a **subset check**: every expected string must appear in the actual `Msg()` args (matched by stringifying each actual arg with `fmt.Sprint`), but extra actual args not listed in `args=` are allowed.

Design rationale: `args=` targets the structured arguments of an error message (e.g. type names, field names), not the rendered text. Internationalized error messages may change wording while keeping the same structured args. `contains=` checks rendered text; `args=` checks structured data. The subset-check design lets authors list only the args they care about (e.g. the two type names in a type-conflict error) without repeating args already covered by `contains=`, and allows checking args whose order may vary across evaluator implementations.

```cue
e: [] & 4   @test(err, code=eval, contains="conflicting values", args=[list, int])
e3: [3][-1] @test(err, code=eval, args=[-1])
```

#### Scenario: args= subset matches
- **WHEN** `@test(err, args=[list, int])` is declared and `Msg()` returns args `["conflicting values []", "list", "int"]` (or any order)
- **THEN** the test passes because both `"list"` and `"int"` are found in the actual args

#### Scenario: args= missing arg fails
- **WHEN** `@test(err, args=[list, int])` is declared but `Msg()` returns only `["list"]`
- **THEN** the test fails reporting that `"int"` was not found

#### Scenario: args= extra actual args are allowed
- **WHEN** `@test(err, args=[int])` is declared and `Msg()` returns `["list", "int"]`
- **THEN** the test passes; the extra `"list"` arg is allowed under the subset-check design

#### Scenario: args= is order-independent
- **WHEN** `@test(err, args=[int, list])` is declared and `Msg()` returns args in order `["list", "int"]`
- **THEN** the test passes regardless of argument order

---

### Requirement: `err` directive — `suberr=(...)` sub-option
The `suberr=(...)` sub-option of `@test(err, ...)` asserts properties of individual sub-errors within a multi-error value (e.g., a failed disjunction). Each `suberr=(...)` entry matches one sub-error; multiple entries match sub-errors order-independently. The body of `suberr=(...)` accepts the same sub-options as `@test(err, ...)`: `code=`, `contains=`, `pos=`, `args=`.

Matching is two-pass:
1. Specs with non-empty `pos=` are matched first against sub-errors with matching positions (position is a stronger discriminator than text content).
2. Remaining specs are matched by `contains=` against unmatched actual sub-errors.

`pos=[]` placeholders inside `suberr=(...)` trigger write-back on `CUE_UPDATE=1`. All position updates for the same `@test` attribute are applied atomically.

```cue
x: null | {n: 3}
x: #empty & {n: 3} @test(err, code=eval,
    suberr=(contains="conflicting values", args=[struct, null]),
    suberr=(contains="not allowed"))
```

#### Scenario: suberr= matches all sub-errors
- **WHEN** `@test(err, suberr=(contains="A"), suberr=(contains="B"))` is declared and the error has exactly two sub-errors whose messages contain `"A"` and `"B"` respectively
- **THEN** the test passes

#### Scenario: suberr= order-independent matching
- **WHEN** two `suberr=` specs are declared and the actual sub-errors appear in reverse order relative to the specs
- **THEN** the test passes; matching is order-independent

#### Scenario: suberr= unmatched spec fails
- **WHEN** `@test(err, suberr=(contains="A"), suberr=(contains="B"))` is declared but only one sub-error exists (containing "A")
- **THEN** the test fails reporting that the `suberr=(contains="B")` spec was unmatched

#### Scenario: suberr= pos= placeholder fills on CUE_UPDATE
- **WHEN** `suberr=(pos=[])` appears inside a `@test(err, ...)` and `CUE_UPDATE=1` is set
- **THEN** the runner fills in the actual positions within the `suberr=(...)` body

---

### Requirement: `shareID` directive
The `shareID=name` argument, used inside the expected struct of a `@test(eq, {...})` body, asserts that all fields annotated with the same `name` share the same underlying `*adt.Vertex` (pointer identity after following indirections). This verifies that the evaluator's vertex-sharing optimization is working correctly.

Rules:
- The **first** field carrying a given `shareID` name has its `eq` check run normally — so every value-expression pair is validated at least once.
- **Subsequent** fields with the same `shareID` name skip the `eq` check — their expression is treated as documentation only.
- The pointer-identity assertion runs after all `eq` checks complete.
- `@test(shareID=name)` may only appear inside a `@test(eq, {...})` body; it is not valid as a standalone field attribute.

```cue
a: {x: 1}
b: a
@test(eq, {
    a: {x: 1} @test(shareID=A)   // first occurrence: eq check runs normally
    b: a       @test(shareID=A)   // subsequent occurrence: eq check skipped
})
```

#### Scenario: shareID first occurrence runs eq check
- **WHEN** `a: {x: 1} @test(shareID=A)` is the first field with `shareID=A` and `a` does not equal `{x: 1}`
- **THEN** the test fails on the eq check

#### Scenario: shareID subsequent occurrence skips eq check
- **WHEN** `b: a @test(shareID=A)` is the second field with `shareID=A` and the expression `a` would not be valid in this scope
- **THEN** the eq check is skipped; only the pointer-identity assertion runs

#### Scenario: shareID pointer identity fails
- **WHEN** `a` and `b` are annotated with the same `shareID=A` but point to different `*adt.Vertex` objects
- **THEN** the test fails reporting that the vertices are not shared

#### Scenario: shareID pointer identity passes
- **WHEN** `b: a` (a reference) and `a` are annotated with `shareID=A` and the evaluator has shared the vertex
- **THEN** the test passes

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

### Requirement: `allows` directive
The `allows` directive SHALL assert that `val.Allows(sel)` returns the expected
boolean for the given selector expression. `@test(allows, sel)` asserts allowed=true;
`@test(allows=false, sel)` asserts allowed=false.

The selector is taken from the raw (pre-unquoting) attribute text:
- `string` (unquoted) — any-string pattern (`cue.AnyString`)
- `int` (unquoted) — any-index pattern (`cue.AnyIndex`)
- Anything else is passed to `cue.ParsePath` as a single-selector path:
  - A plain identifier (`foo`) — a regular field by name
  - A quoted string (`"foo"`) — a string field with literal name `foo`; use `"string"` or `"int"` to select a field literally named `string` or `int`
  - A definition name (`#Def`) — a definition field

#### Scenario: Open struct allows any field
- **WHEN** a field carries `@test(allows, "b")` and the struct is open
- **THEN** the test passes (open structs allow any field)

#### Scenario: Closed struct allows known field
- **WHEN** a field carries `@test(allows, "a")` and the closed struct allows `a`
- **THEN** the test passes

#### Scenario: Closed struct denies unknown field
- **WHEN** a field carries `@test(allows=false, "b")` and the closed struct does not allow `b`
- **THEN** the test passes

#### Scenario: Missing selector argument
- **WHEN** a field carries `@test(allows)` with no selector argument
- **THEN** the test fails with a descriptive error

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
The `todo` directive marks a test case as *expected to fail*. Unlike `skip`, the directives on the annotated field **still run** — but failures are suppressed (logged, not reported as errors). If all directives on the field pass, the runner emits a warning indicating that the `@test(todo)` may no longer be needed.

This is useful when a test case is known to be broken but the fix is expected soon, and the author wants to be alerted automatically when it starts passing.

An optional `p=N` priority key-value arg classifies urgency: `p=0` is critical (must fix immediately), `p=1` is important (fix soon), `p=2` is good to have (fix when convenient). An optional `why="reason"` key-value arg provides a human-readable explanation.

`@test(todo)` applies only to the field or struct in which it is defined, not the entire file.

```cue
// Directive still runs; failures suppressed; warns when it passes.
result: somethingBroken @test(eq, 42) @test(todo, p=1, why="evaluator bug #123")
```

#### Scenario: Todo suppresses failure, logs diagnostic
- **WHEN** a test case carries `@test(todo)` and the other directives on the field fail
- **THEN** the test does NOT fail; instead the runner logs a message indicating the test is still failing

#### Scenario: Todo warns when passing
- **WHEN** a test case carries `@test(todo, why="...")` and all other directives on the field pass
- **THEN** the runner logs a WARNING that the TODO is no longer needed, prompting the author to remove `@test(todo)`

#### Scenario: Todo with priority signal
- **WHEN** a test case carries `@test(todo, p=0, why="broken by recent evaluator change")`
- **THEN** the diagnostic log includes the priority and reason

---

### Requirement: `@test(eq:todo, X)` — expected-future-value
The version-qualifier slot of the `eq` directive MAY carry the token `todo` instead of a real version identifier. `@test(eq:todo, X)` marks `X` as the *expected future value* — what the field is expected to produce once a known issue is resolved. It differs from `@test(eq, X)` as follows:

- `@test(eq:todo, X)` runs regardless of the current evaluator version.
- A mismatch is **not** a test failure — it is logged as "still failing".
- A **match** emits a WARNING that the `eq:todo` may be upgraded to a plain `@test(eq, X)`.

`@test(eq:todo, X)` is additive: it does NOT replace any `@test(eq, Y)` on the same field. Both run independently.

```cue
// Current broken value; expected future value annotated.
v: {a: struct.MaxFields(2) & {}}.a
    @test(eq, {})          // current passing assertion
    @test(eq:todo, struct.MaxFields(2) & {})  // future expected value, no failure today
```

#### Scenario: eq:todo still failing — not an error
- **WHEN** a field carries `@test(eq:todo, X)` and the value does not match `X`
- **THEN** the test does NOT fail; the runner logs "TODO eq:todo still failing" with details

#### Scenario: eq:todo now passes — warning emitted
- **WHEN** a field carries `@test(eq:todo, X)` and the value now matches `X`
- **THEN** the runner logs a WARNING that the annotation may be upgraded to `@test(eq, X)`

#### Scenario: eq:todo and eq coexist
- **WHEN** a field carries both `@test(eq, Y)` and `@test(eq:todo, X)` where `Y ≠ X`
- **THEN** `@test(eq, Y)` runs normally; `@test(eq:todo, X)` runs as an expected-to-fail check; both are independent

---

### Requirement: `err:todo` — expected-future-error
The version-qualifier slot of the `err` directive MAY carry the token `todo` instead of a real version identifier. `@test(err:todo, ...)` marks an error assertion as expected-to-fail — useful when a field is *known not to produce an error yet*, but is expected to once a bug is fixed. It differs from `@test(err, ...)` as follows:

- `@test(err:todo, ...)` runs regardless of the current evaluator version.
- A mismatch (field does not produce the expected error) is **not** a test failure — it is logged as "still failing".
- A **match** (field now produces the expected error) emits a WARNING that the `:todo` may be upgraded to a plain `@test(err, ...)`.

`@test(err:todo, ...)` is additive: it does NOT replace any `@test(err, ...)` on the same field. Both run independently.

```cue
// Field currently evaluates to 42 (no error), but is expected to fail
// once the evaluator enforces the constraint.
x: 42 @test(eq, 42, incorrect) @test(err:todo, p=1, code=eval)
```

#### Scenario: err:todo still failing — not an error
- **WHEN** a field carries `@test(err:todo, code=eval)` and the field does not currently produce an eval error
- **THEN** the test does NOT fail; the runner logs "TODO err:todo still failing" with details

#### Scenario: err:todo now passes — warning emitted
- **WHEN** a field carries `@test(err:todo, code=eval)` and the field now produces the expected eval error
- **THEN** the runner logs a WARNING that the annotation may be upgraded to `@test(err, code=eval)`

#### Scenario: err:todo and err coexist
- **WHEN** a field carries both `@test(err, code=incomplete)` (passing) and `@test(err:todo, code=eval)`
- **THEN** `@test(err, code=incomplete)` runs normally; `@test(err:todo, code=eval)` runs as expected-to-fail; both are independent

---

### Requirement: `incorrect` universal modifier
Any assertion directive (`eq`, `err`, `leq`, `kind`, `closed`, etc.) MAY carry `incorrect` as a positional flag. This marks the assertion as documenting the current *known-incorrect* behavior of the field — that is, the value or property the evaluator currently produces even though it is wrong.

Behavior when the assertion runs:
- **If the assertion passes** (the documented incorrect value still matches): the test does NOT fail; the runner logs `NOTE: ... matches (documented as known incorrect behavior)`.
- **If the assertion fails** (the behavior has changed): the test **DOES FAIL**. Any change to the incorrect value needs attention — it may be a welcome fix or a new regression.

The typical usage is to pair `incorrect` with a `:todo` counterpart that documents the expected correct behavior:

```cue
// Documents that the field currently produces 42 (wrong), and that it
// should eventually produce an error.
x: 42 @test(eq, 42, incorrect) @test(err:todo, p=1, code=eval)
```

#### Scenario: incorrect passes — note logged
- **WHEN** a field carries `@test(eq, 42, incorrect)` and evaluates to `42`
- **THEN** the test does NOT fail; the runner logs a NOTE that the known-incorrect behavior is still present

#### Scenario: incorrect fails — test fails
- **WHEN** a field carries `@test(eq, 42, incorrect)` and evaluates to `43`
- **THEN** the test **FAILS** — a change to the incorrect value needs attention (may be a fix or a new regression)

#### Scenario: incorrect applies to err directive — passes
- **WHEN** a field carries `@test(err, code=eval, incorrect)` and the field is an eval error
- **THEN** the test does NOT fail; the runner logs a NOTE that the documented incorrect error behavior is still present

#### Scenario: incorrect applies to err directive — fails
- **WHEN** a field carries `@test(err, code=eval, incorrect)` and the field does NOT produce an eval error
- **THEN** the test **FAILS** — the behavior changed and needs attention

#### Scenario: incorrect does not affect isTodo
- **WHEN** a field carries `@test(eq:todo, 99)` (no `incorrect`): normal `:todo` logging applies
- **THEN** the `:todo` semantics are unaffected by `incorrect` (which is only checked on the outer directive)

---

### Requirement: `p=N` universal priority modifier
Any `:todo` directive (`eq:todo`, `err:todo`, `@test(todo)`, etc.) MAY carry a `p=N` key-value argument that indicates fix priority. `p=0` is critical (must fix immediately), `p=1` is important (fix soon), `p=2` is good to have (fix when convenient). Higher integers indicate lower urgency.

The priority is **purely informational** — it is shown in log output but has no effect on pass/fail behavior.

```cue
x: 42 @test(eq:todo, 99, p=1)
x: 1/0 @test(err:todo, p=0, code=eval)
result: bad @test(eq, bad) @test(todo, p=2, why="low-priority cleanup")
```

#### Scenario: p=N appears in log output
- **WHEN** a field carries `@test(err:todo, p=1, code=eval)` and the assertion is still failing
- **THEN** the runner logs the failure with `p=1` included in the message

#### Scenario: p=N has no effect on pass/fail
- **WHEN** a field carries `@test(eq:todo, 99, p=0)` and the assertion is still failing
- **THEN** the test does NOT fail regardless of the priority value; `p=0` only affects log formatting

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
The `permuteCount` directive asserts the total number of permutations that were executed for the test case root. It is placed alongside `@test(permute)` (either in the same struct as a decl attr, or on the parent struct). When `CUE_UPDATE=1` is set, the count is auto-filled if empty or replaced if it differs.

```cue
in: {
    @test(permute)
    a: 1, b: a+1
    @test(permuteCount, 2)
}
```

For multiple permutation groups within one test root, `@test(permuteCount, N)` at the root asserts the total across all groups:

```cue
multiPermute: {
    x: { @test(permute); alpha: 1, beta: 2, gamma: 3 }
    y: { @test(permute); alpha: 1, beta: 2, gamma: 3 }
    @test(permuteCount, 12) // 2 groups × 3! = 12 total
}
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

### Requirement: `debug` directive
The `debug` directive is an informational annotation that records the debug-printer output of the evaluated value. Unlike `debugCheck`, a mismatch does NOT fail the test — it only logs a difference and auto-updates when `CUE_UPDATE=1` is set.

This is useful for documenting what internal representation a value produces without locking the test to that exact representation.

```cue
result: {a: 1} @test(debug, """<debug output here>""")
```

#### Scenario: Debug output annotation — no test failure on mismatch
- **WHEN** a field carries `@test(debug, "...")` and the actual debug output differs
- **THEN** the difference is logged but the test does not fail

---

### Requirement: Empty `@test()` as placeholder
A field carrying `@test()` (empty attribute body) SHALL be treated as an unfilled assertion placeholder. When `CUE_UPDATE=1` or `CUE_UPDATE=force` is set, the runner SHALL evaluate the field and rewrite the attribute to `@test(eq, <actual_value>)`. Without `CUE_UPDATE`, an empty `@test()` SHALL cause the test to fail with a message prompting the author to run `CUE_UPDATE=1`.

#### Scenario: Scaffold assertion via CUE_UPDATE
- **WHEN** a field carries `@test()` and `CUE_UPDATE=1` is set
- **THEN** the txtar source file is updated with the evaluated value as an `@test(eq, ...)` assertion

---

### Requirement: Regression guard for failing `eq` assertions
When `CUE_UPDATE=1` is set and an `eq` assertion **fails** (genuine mismatch), the runner SHALL fail the test. It SHALL NOT silently overwrite or skip the expected value.

`CUE_UPDATE=force` SHALL annotate the attribute with `skip:<version>` to mark the discrepancy and let the test pass while the difference is tracked. The nominal expected value is preserved; only the skip marker is added.

When a `skip`-annotated assertion now passes again under `CUE_UPDATE=1`, the runner SHALL remove the stale `skip:` argument, restoring the plain `@test(eq, <expr>)` form.

#### Scenario: Failing assertion is an error under CUE_UPDATE=1
- **WHEN** `@test(eq, 42)` fails because the value is `43` and `CUE_UPDATE=1` is set
- **THEN** the test fails; the source file is NOT modified

#### Scenario: Mark failing assertion with CUE_UPDATE=force
- **WHEN** `@test(eq, 42)` fails because the value is `43` and `CUE_UPDATE=force` is set
- **THEN** the attribute is rewritten to `@test(eq, 42, skip:v3)` so the test is skipped for that version

#### Scenario: Remove stale skip on recovery
- **WHEN** `@test(eq, 42, skip:v3)` now passes and `CUE_UPDATE=1` is set
- **THEN** the `skip:v3` argument is removed, leaving `@test(eq, 42)`

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

| Section present? | `CUE_UPDATE=1` | Normal run | `CUE_UPDATE=diff` |
|-----------------|---------------|------------|-------------------|
| No              | skip (don't create) | skip | skip |
| Yes             | update silently | skip (no fail) | show diff (no fail) |

Key points:
- The section is **never auto-created** by `CUE_UPDATE=1`. It must be added manually.
- A normal test run silently ignores differences in this section.
- `CUE_UPDATE=diff` shows a unified diff of what `CUE_UPDATE=1` would write, without modifying files.
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

#### Scenario: Section present — CUE_UPDATE=diff shows diff on stale content
- **WHEN** the archive has an `-- out/errors.txt --` section with stale content and `CUE_UPDATE=diff` is set
- **THEN** the runner shows a unified diff of the expected and actual error output but does not fail the test
