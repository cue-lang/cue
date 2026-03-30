# Test Migration Tool

## Requirements

### Requirement: Migration tool converts golden-file tests to inline assertions
The repository SHALL include a best-effort migration tool (in `internal/cuetxtar/cmd/migrate` or a standalone script) that reads a golden-file txtar archive and produces an equivalent archive in inline-assertion mode. The tool operates on a single file at a time and MUST NOT modify `out/compile` golden sections.

#### Scenario: Basic conversion
- **WHEN** the migration tool is run on a txtar file that has `out/eval` and `out/compile` sections
- **THEN** the tool emits a new txtar file with `@test(...)` attributes on the CUE source fields and the `out/eval` section removed, while the `out/compile` section is preserved unchanged

---

### Requirement: Error heuristic detection
The migration tool SHALL detect fields in the golden output that are marked as errors (e.g., lines containing `(_|_)` or `// Error` comments) and generate corresponding `@test(err, any)` attributes on the parent struct or `@test(err)` on the specific field. Where the error message or code can be parsed, the tool SHALL generate more precise assertions (e.g., `@test(err, code=cycle)`).

#### Scenario: Cycle error detection
- **WHEN** the golden output shows a field with a cycle error
- **THEN** the migration tool generates `@test(err, code=cycle)` on that field or `@test(err, path=(...), code=cycle)` if ambiguity exists

#### Scenario: Unknown error type falls back to `any`
- **WHEN** the golden output shows an error but the tool cannot determine the code
- **THEN** the migration tool generates `@test(err, any)` at the nearest enclosing test-case root

---

### Requirement: Version-specific golden output produces versioned assertions
The migration tool SHALL apply the following version-handling rule when reading golden sections:

- **`out/evalalpha` only**: Use it as the base (unversioned) `eq` assertion.
- **`out/eval` only**: Use it as the base (unversioned) `eq` assertion.
- **Both `out/eval` and `out/evalalpha`**: `out/evalalpha` (v3) becomes the base `eq`; `out/eval` (v2) becomes a `v2: { eq: ... }` version sub-struct override.

This rule reflects that v3 is the forward-looking baseline. V2 divergences are captured explicitly so they remain visible as known differences rather than silent regressions.

#### Scenario: Only evalalpha section exists
- **WHEN** a txtar file has an `out/evalalpha` section but no `out/eval` section
- **THEN** the migration tool generates `@test(eq, <evalalpha_value>)` as the unversioned base assertion

#### Scenario: Only eval section exists
- **WHEN** a txtar file has an `out/eval` section but no `out/evalalpha` section
- **THEN** the migration tool generates `@test(eq, <eval_value>)` as the unversioned base assertion

#### Scenario: Divergent versions
- **WHEN** `out/eval` and `out/evalalpha` both exist and produce different results for the same field
- **THEN** the migration tool generates `@test(eq, <evalalpha_value>)` as the unversioned base and a `v2: { eq: <eval_value> }` version sub-struct override

---

### Requirement: File splitting into structural containers
Many existing txtar files pack multiple independent scenarios as nested sub-fields of a single top-level field. The migration tool SHALL detect common patterns and restructure them as distinct top-level structural test containers. Detection is heuristic; the tool SHALL produce valid output even when heuristics misfire (using `@test(/* TODO: review */)` when uncertain).

Recognized patterns:

| Pattern | Example nested fields | Action |
|---------|----------------------|--------|
| Numbered sub-tests | `t1`, `t2`, `t3` | Each becomes a separate structural container with `in: original_sub_field` |
| Pass/fail pairs | `ok`, `err` | Split into separate `ok` and `err` containers |
| Permutation groups | `p1`, `p2`, `p3` | Collapse into one container with canonical field ordering and `@test(permute)` on `in` (see permute requirement below) |
| Per-letter variants | `a`, `b`, `d`, `e`, `f`, `g` | Best-effort: tool applies judgment to merge into permuted groups, rename to descriptive identifiers, or keep as separate containers |

#### Scenario: Numbered sub-tests split into separate containers
- **WHEN** a top-level field contains sub-fields `t1` and `t2` representing independent scenarios
- **THEN** the migration tool emits two separate structural containers, each with `in:` holding the respective sub-field value

#### Scenario: Permutation group collapsed
- **WHEN** sub-fields `p1`, `p2`, `p3` contain the same set of fields in different orders with identical expected results
- **THEN** the migration tool emits a single container with `@test(permute)` on `in` and a shared `eq:` field

---

### Requirement: `@test(permute)` generation for permutation groups
When the migration tool identifies a permutation group (same set of fields, different orderings, identical evaluated result under both evaluator versions), it SHALL:

1. Emit a single structural container with one canonical field ordering in `in`.
2. Add `@test(permute)` as a decl attribute inside `in`.
3. Emit the expected result in `eq`.
4. If results differ across evaluator versions, apply the standard version-handling rule to generate appropriate version sub-structs.

#### Scenario: Permutation group with identical results
- **WHEN** `p1`, `p2`, `p3` produce the same evaluated result in both `out/eval` and `out/evalalpha`
- **THEN** the migration tool emits one structural container with `@test(permute)` on `in` and a single unversioned `eq:`

#### Scenario: Permutation group with version-divergent results
- **WHEN** a permutation group has the same result across permutations but differs between `out/eval` and `out/evalalpha`
- **THEN** the migration tool emits one structural container with `@test(permute)` on `in`, `eq:` from `out/evalalpha`, and `v2: { eq: ... }` from `out/eval`

---

### Requirement: Migration is best-effort with manual review marker
The migration tool SHALL mark fields where it could not confidently generate an assertion with a `@test(/* TODO: review */)` placeholder comment. This signals to the author that manual review is needed.

#### Scenario: Ambiguous output flagged for review
- **WHEN** the migration tool encounters output it cannot parse into a structured assertion
- **THEN** it emits `@test(/* TODO: review */)` on the relevant field and prints a warning to stderr

---

### Requirement: Dry-run mode
The migration tool SHALL support a `--dry-run` flag that prints the proposed changes to stdout without modifying any files.

#### Scenario: Dry run does not modify files
- **WHEN** the migration tool is invoked with `--dry-run`
- **THEN** the modified txtar content is written to stdout and no files on disk are changed
