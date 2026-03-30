# Test Migration Guidelines

These are guidelines for migrating golden-file txtar tests to inline `@test`
assertions. They are not a formal requirement for a tool; use them as a manual
checklist or the basis for ad-hoc tooling.

---

## Guideline: Basic conversion

A txtar archive in golden-file mode can be converted to inline-assertion mode
by:

1. Parsing the `out/eval` or `out/evalalpha` section to derive expected values.
2. Adding `@test(eq, <value>)` field attributes (or decl attributes) directly
   in the `.cue` source.
3. Removing the `out/eval` section after conversion (the `out/compile` section,
   if present, should remain unchanged).

---

## Guideline: Error detection

Fields whose golden output contains `(_|_)` or `// Error` markers should be
annotated with `@test(err, ...)`. Where the error code can be determined from
the golden output, use `code=<code>`. When uncertain, use `@test(err, any)` on
the nearest enclosing struct or `@test(err)` on the specific field.

---

## Guideline: Version-specific golden output

When both `out/eval` (v2) and `out/evalalpha` (v3) sections exist:

- If they produce the **same result**: use the v3 output as the unversioned
  `eq` assertion.
- If they **diverge**: use the v3 output as the unversioned `eq` assertion and
  record the v2 difference as a versioned `@test(eq:v2, ...)` attribute.

This makes v3 the forward-looking baseline while keeping v2 divergences visible.

---

## Guideline: Permutation groups

Sub-fields `p1`, `p2`, `p3` (etc.) that contain the same fields in different
orders and produce identical evaluated results can be collapsed into a single
test case with `@test(permute)` on the relevant fields or as a decl attribute
inside the struct.

---

## Guideline: Fixture fields

Top-level fields that are shared helpers and not test cases in their own right
should be placed in a separate `.cue` fixture file (or a file with no `@test`
attributes). A file with no `@test` attributes anywhere is treated as a pure
fixture file and its fields are available to test files without registration as
sub-tests.
