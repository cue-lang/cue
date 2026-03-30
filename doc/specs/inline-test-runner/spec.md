# Inline Test Runner

## Requirements

### Requirement: Inline test runner function
The package `internal/cuetxtar` SHALL expose a function `RunInlineTests` that accepts a `*testing.T`, a `cuetdtest.Matrix`, and a root test directory. It SHALL iterate over txtar archives in the directory (using the same traversal as `TxTarTest`), detect inline-assertion mode (presence of any `@test(...)` field attribute or any decl `@test(...)` attribute on a top-level struct field in any `.cue` file in the archive), and dispatch accordingly.

#### Scenario: Inline file dispatched to inline runner
- **WHEN** a txtar archive contains a `.cue` file with at least one top-level field carrying any `@test(...)` field attribute, or any top-level struct field with a decl `@test(...)` attribute
- **THEN** `RunInlineTests` processes that file in inline-assertion mode

#### Scenario: Golden-file archive unchanged
- **WHEN** a txtar archive contains no `@test(...)` attributes anywhere
- **THEN** `RunInlineTests` does not process it (the existing `TxTarTest.Run` handles it)

---

### Requirement: Per-test-case sub-tests
For each test-case root — either a top-level field with a `@test(...)` field attribute (inline form) or a top-level struct with a decl `@test(...)` attribute (structural form) — the runner SHALL call `t.Run(name, ...)` where `name` is always the CUE field name. `@test(desc="...")` is a description annotation only and does NOT affect the sub-test name. This enables standard Go test filtering via the `-run` flag.

#### Scenario: Inline test case named from field name
- **WHEN** a top-level field `basicCycle: 42 @test(eq, 42)` exists
- **THEN** a sub-test named `"basicCycle"` is registered

#### Scenario: Structural test case named from field name
- **WHEN** a structural container field is named `cycleTest` and carries `@test(desc="cycle between a and b")` as a decl attribute
- **THEN** a sub-test named `"cycleTest"` is registered; the `desc` value is ignored for naming

#### Scenario: Filter by test case
- **WHEN** `go test -run TestEvalV3/tests__myfile__cue/basicCycle` is executed
- **THEN** only the `basicCycle` test case in `myfile.txtar` runs

---

### Requirement: @test attribute extraction and AST stripping
Before compiling or evaluating any CUE in an inline-assertion file, the runner SHALL walk the parsed `ast.File` and extract all `@test(...)` attributes, recording each as an `attrRecord` containing the CUE path, the original `*ast.Attribute` node (which carries source position for `CUE_UPDATE` write-back), and a flag indicating whether the path is relative to `evaluate(in)` of a `#Test` container. The runner SHALL then remove all `@test(...)` attributes from the AST. The CUE compiler and evaluator SHALL receive the stripped AST; no `@test` attribute SHALL be visible to the evaluator.

**Fixture files**: A `.cue` file in the archive that contains no `@test(...)` attributes (after stripping) is treated as a fixture file. Fixture files are compiled first and their fields form a **scope** for test files (files that do have `@test` attributes). This allows test files to reference fixture-file fields using CUE identifiers without getting "reference not found" errors. Fields in fixture files are exempt from the coverage check.

**Path recording**: the walker maintains a path stack, pushing each struct field's identifier on entry and popping on exit. For inline-form field attrs the recorded path is the full path to the annotated field. For structural-form container decl attrs the path is the container field name, resolved against `evaluate(in)` after evaluation. For attrs on `eq` sub-fields the recorded path is relative to `eq` and is also resolved against `evaluate(in)`.

**Mode detection** is purely syntactic and requires only an AST parse: the walker checks for `@test(...)` field attrs on named fields (inline root) and for decl `@test(...)` attributes on top-level struct fields (structural root). No compilation is required at this stage.

This ensures that tests for CUE code that itself uses `@test` attributes in its schema logic are not affected by the test harness, and that the evaluated result reflects purely the CUE semantics under test.

#### Scenario: Test attribute does not appear in evaluated value
- **WHEN** a field is `result: 42 @test(eq, 42)` in the source
- **THEN** after stripping, the evaluator sees `result: 42` with no attribute, and the assertion is run separately against the evaluated value

#### Scenario: Non-test attributes are preserved
- **WHEN** a field carries both `@json("x")` and `@test(eq, 42)`
- **THEN** the `@json("x")` attribute remains in the AST; only `@test(eq, 42)` is stripped

#### Scenario: CUE code that uses @test attributes can be tested
- **WHEN** a test case defines a schema field `x: int @test("something")` and a root assertion `@test(eq, ...)`
- **THEN** the root `@test(eq, ...)` is stripped and run as an assertion; the inner `@test("something")` on `x` is also stripped, so the evaluated schema does not include it

---

### Requirement: Assertion execution by recorded path
After evaluation, the runner SHALL look up each recorded `(path, []directive)` pair by navigating the evaluated `cue.Value` to the path captured during AST extraction. It SHALL then run each directive as an assertion against the value at that path.

#### Scenario: Nested assertion
- **WHEN** a test case contains `inner: {x: 1 @test(eq, 1)}`
- **THEN** the runner evaluates the assertion at the path `inner.x` of the evaluated value

---

### Requirement: Version discrimination
The runner SHALL receive the current evaluator version token (from `cuetdtest.M`) and match it against versioned directive suffixes. An assertion with `directive:vN` SHALL be evaluated only when the current version matches `vN`. An unversioned assertion SHALL be evaluated for all versions, except when a versioned form for the current version is also present (the versioned form takes precedence).

#### Scenario: Versioned directive overrides unversioned
- **WHEN** a field carries `@test(eq, 1) @test(eq:v3, 2)` and the runner is using v3
- **THEN** only `eq:v3` is evaluated; `eq` is ignored for this version

#### Scenario: Unversioned applies when no version-specific form
- **WHEN** a field carries `@test(eq, 1)` and no versioned form exists for the current version
- **THEN** `eq` is evaluated

---

### Requirement: Structural form evaluation
When a test-case root is a structural container (detected by the presence of a decl `@test(...)` attribute), the runner SHALL:
1. Check whether a version sub-struct exists for the current evaluator version (a field matching `[=~"^v"]` whose name matches the version token). If present, its `eq`, `leq`, `legacy`, `diff`, and `skip` fields SHALL override the corresponding top-level fields.
2. If `skip` (effective after version override) is true, call `t.Skip` and stop.
3. Evaluate `in` (with all `@test` attributes stripped).
4. If `eq` is present: compare `evaluate(in)` against `evaluate(eq)` (stripped) using `internal/core/diff` configured to ignore error message text. Process any `@test(shareId=...)` field attributes on `eq` sub-fields as sharing constraints on the corresponding fields in `evaluate(in)`. Process any `@test(err, ...)` field attributes on `eq` sub-fields as error assertions on the corresponding fields in `evaluate(in)`.
5. If `leq` is present: assert `evaluate(in)` is subsumed by `evaluate(leq)` using `value.Subsumes`.
6. Process all decl `@test(err, ...)` attributes on the container as error assertions over `evaluate(in)` or its descendants.
7. If `legacy` is present: compare the debug printer output of `evaluate(in)` against the `legacy` string value (see `legacy:` sub-field requirement).

When `CUE_UPDATE=diff` is set and a structural form test fails, the runner SHALL write `diff` and `skip: true` into the appropriate version sub-struct (creating the sub-struct if it does not yet exist).

#### Scenario: Version sub-struct overrides top-level eq
- **WHEN** a `#Test` container has `eq: { result: 1 }` and `v3: { eq: { result: 2 } }` and the runner is using v3
- **THEN** `v3.eq` is used for the equality check; `eq` at the top level is ignored

#### Scenario: Version sub-struct skip
- **WHEN** a `#Test` container has `v3: { skip: true }` and the runner is using v3
- **THEN** the test is skipped; it runs normally under other versions

#### Scenario: CUE_UPDATE=diff writes to version sub-struct
- **WHEN** a structural test fails under v3 and `CUE_UPDATE=diff` is set
- **THEN** a `v3: { diff: "got ...; want ...", skip: true }` sub-struct is written into the CUE source

---

### Requirement: Structured failure messages
When an assertion fails, the runner SHALL report the failure via `t.Error` (not `t.Fatal`), so that all assertions within a test case are evaluated and reported in one pass. The failure message SHALL include the CUE path of the failing field, the directive that failed, the actual value, and the expected value or error.

#### Scenario: All assertions reported in one run
- **WHEN** a test case has three assertions and two of them fail
- **THEN** both failures are reported; the test does not stop after the first failure

---

### Requirement: Compile golden sections unaffected
The runner SHALL NOT affect `out/compile` golden sections in txtar archives. Compilation tests continue to use the golden-file mechanism regardless of whether the archive is in inline-assertion mode for eval tests.

#### Scenario: Compile section preserved in inline-mode file
- **WHEN** a txtar archive is in inline-assertion mode and also has an `out/compile` section
- **THEN** the eval assertions are evaluated inline and the compile section is compared as a golden file, both in the same test run

---

### Requirement: CUE_UPDATE rewrite integration
When `CUE_UPDATE=1`, `CUE_UPDATE=diff`, or `CUE_UPDATE=force` is set, the runner SHALL perform in-place rewrites of `@test(...)` attributes in the txtar source as specified in the `inline-test-attributes` spec. After all tests in a txtar file have run, the modified archive SHALL be written back to disk atomically.

#### Scenario: Txtar written back after fill
- **WHEN** `CUE_UPDATE=1` is set and an empty `@test()` is filled in
- **THEN** the txtar file on disk is updated with the new `@test(eq, ...)` attribute

---

### Requirement: `#subpath` tag for scoped runs
A txtar archive MAY include a `#subpath: <fieldName>` tag in its comment header. When present, the runner SHALL restrict processing to the test-case root at the named field, ignoring all other roots. This supports focused debugging without modifying the file.

#### Scenario: Subpath restricts execution
- **WHEN** a txtar file has `#subpath: basicCycle` and two test-case roots named `basicCycle` and `typeCheck`
- **THEN** only the `basicCycle` test runs

---

### Requirement: `file` directive evaluation
When a field carries `@test(file, "name.cue")`, the runner SHALL read the named txtar section, parse it as a CUE expression, and compare it against the evaluated field value using `internal/core/diff` with error message text ignored (same semantics as `eq`). After parsing the expected file, the runner SHALL also extract any `@test(shareId=name)` annotations within it and evaluate them as additional sharing constraints against the corresponding positions in the evaluated result (see the `shareId` in expected-value files requirement in `inline-test-attributes`).

When `CUE_UPDATE=1` is set and the section does not exist or the value has changed, the runner SHALL write the actual evaluated value to the named section, subject to the standard regression-guard rules.

#### Scenario: File content matches
- **WHEN** `@test(file, "expected/r.cue")` is declared and the evaluated value equals the file content
- **THEN** the test passes

#### Scenario: File section created on CUE_UPDATE
- **WHEN** `@test(file, "expected/r.cue")` is declared, the section is absent, and `CUE_UPDATE=1` is set
- **THEN** the section is added to the txtar archive with the actual evaluated value

#### Scenario: shareId annotations in file are checked
- **WHEN** the file contains `a: {x:1} @test(shareId=t1)` and `b: {x:1} @test(shareId=t1)` and the evaluated `a` and `b` do not share a vertex
- **THEN** the test fails with a sharing violation in addition to any value comparison result

---

### Requirement: `legacy:` sub-field evaluation
When a structural test container includes a `legacy:` sub-field, the runner SHALL invoke `internal/core/debug`'s standard printer on the evaluated `in` value and compare the output **textually** (string equality, ignoring only trailing whitespace) against the string value of `legacy:`. A mismatch SHALL be reported via `t.Error` with a textual diff.

When `CUE_UPDATE=1` is set, the runner SHALL fill or update the `legacy:` field value with the actual printer output, subject to the standard regression-guard rules. No separate txtar sections are created or modified for `legacy:` comparisons.

#### Scenario: legacy matches printer output
- **WHEN** a structural container has a `legacy:` field and the debug printer output of the evaluated `in` equals the `legacy:` string
- **THEN** the test passes

#### Scenario: legacy mismatch reported
- **WHEN** the debug printer output differs from the `legacy:` string
- **THEN** the runner reports `t.Error` with a diff between actual and expected

#### Scenario: CUE_UPDATE fills legacy field
- **WHEN** `legacy: ""` is present and `CUE_UPDATE=1` is set
- **THEN** the `legacy:` field in the CUE source is updated with the actual printer output
