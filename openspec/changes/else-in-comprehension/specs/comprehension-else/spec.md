## ADDED Requirements

### Requirement: Else clause syntax

The parser SHALL accept an `else` keyword followed by a StructLit as an optional terminal clause in a comprehension. The grammar SHALL be:

```
Comprehension       = Clauses StructLit [ ElseClause ] .
ElseClause          = "else" StructLit .
```

#### Scenario: Parse if comprehension with else
- **WHEN** the input is `if enabled { a: 1 } else { b: 2 }`
- **THEN** the parser SHALL produce an AST with an IfClause, a StructLit body, and an ElseClause with its own StructLit

#### Scenario: Parse for comprehension with else
- **WHEN** the input is `for x in list { (x): true } else { empty: true }`
- **THEN** the parser SHALL produce an AST with a ForClause, a StructLit body, and an ElseClause with its own StructLit

#### Scenario: Parse multi-clause comprehension with else
- **WHEN** the input is `for x in list if x > 0 let y = x * 2 { (y): x } else { none: true }`
- **THEN** the parser SHALL produce an AST with ForClause, IfClause, LetClause, a StructLit body, and an ElseClause

#### Scenario: Comprehension without else remains valid
- **WHEN** the input is `if enabled { a: 1 }`
- **THEN** the parser SHALL produce an AST with an IfClause and a StructLit body, with no ElseClause

### Requirement: Single else clause limit

A comprehension SHALL have at most one else clause. If multiple else clauses are present, the parser SHALL report an error.

#### Scenario: Multiple else clauses rejected
- **WHEN** the input is `if enabled { a: 1 } else { b: 2 } else { c: 3 }`
- **THEN** the parser SHALL report an error indicating multiple else clauses are not allowed

### Requirement: Else token

The lexer SHALL recognize `else` as a keyword token.

#### Scenario: Else as keyword
- **WHEN** the input contains the identifier `else`
- **THEN** the lexer SHALL emit an ELSE token, not an IDENT token

### Requirement: If-else semantics

When a comprehension has an if clause and an else clause, the else clause's StructLit SHALL be yielded exactly once if and only if the comprehension yields zero values.

#### Scenario: If condition true - no else
- **WHEN** evaluating `{ if true { a: 1 } else { b: 2 } }`
- **THEN** the result SHALL be `{ a: 1 }`

#### Scenario: If condition false - else yields
- **WHEN** evaluating `{ if false { a: 1 } else { b: 2 } }`
- **THEN** the result SHALL be `{ b: 2 }`

#### Scenario: If with non-boolean condition
- **WHEN** evaluating `{ if x { a: 1 } else { b: 2 } }` where `x` is not a boolean
- **THEN** the evaluation SHALL report a type error (existing behavior, unaffected by else)

### Requirement: For-else semantics

When a comprehension starts with a for clause and has an else clause, the else clause's StructLit SHALL be yielded exactly once if and only if the for loop (including any subsequent filtering if clauses) yields zero values.

#### Scenario: For with non-empty source - no else
- **WHEN** evaluating `{ for x in [1, 2] { "\(x)": x } else { empty: true } }`
- **THEN** the result SHALL be `{ "1": 1, "2": 2 }`

#### Scenario: For with empty source - else yields
- **WHEN** evaluating `{ for x in [] { "\(x)": x } else { empty: true } }`
- **THEN** the result SHALL be `{ empty: true }`

#### Scenario: For with filter removing all - else yields
- **WHEN** evaluating `{ for x in [1, 2, 3] if x > 10 { "\(x)": x } else { empty: true } }`
- **THEN** the result SHALL be `{ empty: true }`

#### Scenario: For with filter keeping some - no else
- **WHEN** evaluating `{ for x in [1, 2, 3] if x > 1 { "\(x)": x } else { empty: true } }`
- **THEN** the result SHALL be `{ "2": 2, "3": 3 }`

### Requirement: List comprehension else

The else clause SHALL work in list comprehensions, yielding the else struct's contents as list elements.

#### Scenario: List for-else with empty source
- **WHEN** evaluating `[ for x in [] { x } else { 0 } ]`
- **THEN** the result SHALL be `[0]`

#### Scenario: List for-else with non-empty source
- **WHEN** evaluating `[ for x in [1, 2] { x * 2 } else { 0 } ]`
- **THEN** the result SHALL be `[2, 4]`

#### Scenario: List if-else true
- **WHEN** evaluating `[ if true { 1 } else { 2 } ]`
- **THEN** the result SHALL be `[1]`

#### Scenario: List if-else false
- **WHEN** evaluating `[ if false { 1 } else { 2 } ]`
- **THEN** the result SHALL be `[2]`

### Requirement: Else clause scoping

The else clause SHALL have access to the enclosing scope but SHALL NOT have access to variables bound by for or let clauses within the comprehension.

**Rationale**: When `else` triggers, the comprehension yielded zero values, meaning `for` variables have no meaningful value. While `let` variables that don't depend on `for` variables could theoretically be accessible, allowing partial access creates subtle bugs: code that works when at least one iteration succeeds would fail mysteriously when all iterations are filtered out. A simple, uniform rule (no comprehension-internal variables in `else`) is predictable and avoids these edge cases. Users needing shared values can bind them in the outer scope before the comprehension.

#### Scenario: Else accesses outer scope
- **WHEN** evaluating `{ outer: 1, result: { for x in [] { x } else { fallback: outer } }.result }`
- **THEN** the result SHALL include `result: { fallback: 1 }`

#### Scenario: Else cannot access for variables
- **WHEN** evaluating `{ for x in [] { x } else { bad: x } }`
- **THEN** the evaluation SHALL report an error that `x` is undefined

#### Scenario: Else cannot access let variables
- **WHEN** evaluating `{ for x in [] let y = 1 { y } else { bad: y } }`
- **THEN** the evaluation SHALL report an error that `y` is undefined

### Requirement: Else with nested comprehensions

When comprehensions are nested, each else clause SHALL apply only to its immediately enclosing comprehension.

#### Scenario: Outer else triggers, inner does not
- **WHEN** evaluating `{ for x in [] { for y in [1] { y } else { inner: true } } else { outer: true } }`
- **THEN** the result SHALL be `{ outer: true }` (outer else triggers because outer for is empty)

#### Scenario: Inner else triggers, outer does not
- **WHEN** evaluating `{ for x in [1] { for y in [] { y } else { inner: true } } else { outer: true } }`
- **THEN** the result SHALL be `{ inner: true }` (inner else triggers, outer for yielded)

### Requirement: Else does not trigger on errors

When a comprehension's body produces errors on all iterations, the else clause SHALL NOT be triggered. Errors SHALL propagate normally.

#### Scenario: Error in body does not trigger else
- **WHEN** evaluating `{ for x in [1] { bad: x.nonexistent } else { fallback: true } }`
- **THEN** the evaluation SHALL report an error, not yield `{ fallback: true }`

### Requirement: Else with struct embedding

The else clause's StructLit SHALL be embedded in the enclosing struct, following the same embedding rules as the main comprehension body.

#### Scenario: Else embeds fields
- **WHEN** evaluating `{ existing: 1, if false { added: 2 } else { fallback: 3 } }`
- **THEN** the result SHALL be `{ existing: 1, fallback: 3 }`

#### Scenario: Else can contain multiple fields
- **WHEN** evaluating `{ if false { a: 1 } else { b: 2, c: 3 } }`
- **THEN** the result SHALL be `{ b: 2, c: 3 }`
