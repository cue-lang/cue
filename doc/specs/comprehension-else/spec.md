# Comprehension Fallback Clause

## Purpose

Provides an optional fallback clause for comprehensions that yields a fallback value when the comprehension produces zero values. This enables concise handling of empty collections and failed filter conditions without external conditionals.

## Keywords

The keyword used depends on the comprehension type:
- `for` comprehensions use the `fallback` keyword
- `if` comprehensions use the `else` keyword
- `try` comprehensions use the `else` keyword

This distinction provides semantic clarity: `else` implies a binary choice (true/false, success/failure), while `fallback` indicates a default when no results are produced.

## Requirements

### Requirement: Fallback clause syntax

The parser SHALL accept an `else` keyword followed by a StructLit as an optional terminal clause after `if` or `try` clauses, and a `fallback` keyword followed by a StructLit as an optional terminal clause after `for` clauses. The grammar SHALL be:

```
Comprehension       = Clauses StructLit [ ElseClause | FallbackClause ] .
ElseClause          = "else" StructLit .
FallbackClause      = "fallback" StructLit .
```

#### Scenario: Parse if comprehension with else
- **WHEN** the input is `if enabled { a: 1 } else { b: 2 }`
- **THEN** the parser SHALL produce an AST with an IfClause, a StructLit body, and an ElseClause with its own StructLit

#### Scenario: Parse for comprehension with fallback
- **WHEN** the input is `for x in list { (x): true } fallback { empty: true }`
- **THEN** the parser SHALL produce an AST with a ForClause, a StructLit body, and a FallbackClause with its own StructLit

#### Scenario: Parse multi-clause comprehension with fallback
- **WHEN** the input is `for x in list if x > 0 let y = x * 2 { (y): x } fallback { none: true }`
- **THEN** the parser SHALL produce an AST with ForClause, IfClause, LetClause, a StructLit body, and a FallbackClause

#### Scenario: Comprehension without else or fallback remains valid
- **WHEN** the input is `if enabled { a: 1 }`
- **THEN** the parser SHALL produce an AST with an IfClause and a StructLit body, with no ElseClause or FallbackClause

### Requirement: Keyword-clause validation

The parser SHALL validate that the correct keyword is used based on the preceding clause type:
- `else` is valid only after `if` or `try` clauses
- `fallback` is valid only after `for` clauses

#### Scenario: else after if accepted
- **WHEN** the input is `if enabled { a: 1 } else { b: 2 }`
- **THEN** the parser SHALL accept this as valid

#### Scenario: fallback after if rejected
- **WHEN** the input is `if enabled { a: 1 } fallback { b: 2 }`
- **THEN** the parser SHALL report an error: "use 'else' with 'if' clauses"

#### Scenario: fallback after for accepted
- **WHEN** the input is `for x in list { (x): true } fallback { empty: true }`
- **THEN** the parser SHALL accept this as valid

#### Scenario: else after for rejected
- **WHEN** the input is `for x in list { (x): true } else { empty: true }`
- **THEN** the parser SHALL report an error: "use 'fallback' with 'for' clauses"

#### Scenario: fallback after try rejected
- **WHEN** the input is `try { a: x? } fallback { b: 2 }`
- **THEN** the parser SHALL report an error: "use 'else' with 'try' clauses"

### Requirement: Single else or fallback clause limit

A comprehension SHALL have at most one else or fallback clause. If multiple such clauses are present, the parser SHALL report an error.

#### Scenario: Multiple else clauses rejected
- **WHEN** the input is `if enabled { a: 1 } else { b: 2 } else { c: 3 }`
- **THEN** the parser SHALL report an error indicating multiple else clauses are not allowed

#### Scenario: Multiple fallback clauses rejected
- **WHEN** the input is `for x in list { x } fallback { a: 1 } fallback { b: 2 }`
- **THEN** the parser SHALL report an error indicating multiple fallback clauses are not allowed

### Requirement: Else token

The lexer SHALL recognize `else` as a keyword token.

#### Scenario: Else as keyword
- **WHEN** the input contains the identifier `else`
- **THEN** the lexer SHALL emit an ELSE token, not an IDENT token

### Requirement: Fallback token

The lexer SHALL recognize `fallback` as a keyword token.

#### Scenario: Fallback as keyword
- **WHEN** the input contains the identifier `fallback`
- **THEN** the lexer SHALL emit a FALLBACK token, not an IDENT token

### Requirement: Keywords as field labels

The parser SHALL accept `else` and `fallback` keywords as valid field labels.

#### Scenario: Else and fallback as field labels
- **WHEN** the input contains `else: 1` or `fallback: 2` as field definitions
- **THEN** the parser SHALL accept these as valid field labels

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

### Requirement: For-fallback semantics

When a comprehension starts with a for clause and has a fallback clause, the fallback clause's StructLit SHALL be yielded exactly once if and only if the for loop (including any subsequent filtering if clauses) yields zero values.

#### Scenario: For with non-empty source - no fallback
- **WHEN** evaluating `{ for x in [1, 2] { "\(x)": x } fallback { empty: true } }`
- **THEN** the result SHALL be `{ "1": 1, "2": 2 }`

#### Scenario: For with empty source - fallback yields
- **WHEN** evaluating `{ for x in [] { "\(x)": x } fallback { empty: true } }`
- **THEN** the result SHALL be `{ empty: true }`

#### Scenario: For with filter removing all - fallback yields
- **WHEN** evaluating `{ for x in [1, 2, 3] if x > 10 { "\(x)": x } fallback { empty: true } }`
- **THEN** the result SHALL be `{ empty: true }`

#### Scenario: For with filter keeping some - no fallback
- **WHEN** evaluating `{ for x in [1, 2, 3] if x > 1 { "\(x)": x } fallback { empty: true } }`
- **THEN** the result SHALL be `{ "2": 2, "3": 3 }`

### Requirement: List comprehension else and fallback

The else clause SHALL work in list comprehensions with `if`, and the fallback clause SHALL work in list comprehensions with `for`, yielding the clause's contents as list elements.

#### Scenario: List for-fallback with empty source
- **WHEN** evaluating `[ for x in [] { x } fallback { 0 } ]`
- **THEN** the result SHALL be `[0]`

#### Scenario: List for-fallback with non-empty source
- **WHEN** evaluating `[ for x in [1, 2] { x * 2 } fallback { 0 } ]`
- **THEN** the result SHALL be `[2, 4]`

#### Scenario: List if-else true
- **WHEN** evaluating `[ if true { 1 } else { 2 } ]`
- **THEN** the result SHALL be `[1]`

#### Scenario: List if-else false
- **WHEN** evaluating `[ if false { 1 } else { 2 } ]`
- **THEN** the result SHALL be `[2]`

### Requirement: Fallback clause scoping

The fallback clause SHALL have access to the enclosing scope but SHALL NOT have access to variables bound by for or let clauses within the comprehension.

**Rationale**: When the fallback triggers, the comprehension yielded zero values, meaning `for` variables have no meaningful value. While `let` variables that don't depend on `for` variables could theoretically be accessible, allowing partial access creates subtle bugs: code that works when at least one iteration succeeds would fail mysteriously when all iterations are filtered out. A simple, uniform rule (no comprehension-internal variables in the fallback clause) is predictable and avoids these edge cases. Users needing shared values can bind them in the outer scope before the comprehension.

#### Scenario: Fallback accesses outer scope
- **WHEN** evaluating `{ outer: 1, result: { for x in [] { x } fallback { fallbackField: outer } }.result }`
- **THEN** the result SHALL include `result: { fallbackField: 1 }`

#### Scenario: Fallback cannot access for variables
- **WHEN** evaluating `{ for x in [] { x } fallback { bad: x } }`
- **THEN** the evaluation SHALL report an error that `x` is undefined

#### Scenario: Fallback cannot access let variables
- **WHEN** evaluating `{ for x in [] let y = 1 { y } fallback { bad: y } }`
- **THEN** the evaluation SHALL report an error that `y` is undefined

### Requirement: Else and fallback with nested comprehensions

When comprehensions are nested, each else or fallback clause SHALL apply only to its immediately enclosing comprehension.

#### Scenario: Outer fallback triggers, inner does not
- **WHEN** evaluating `{ for x in [] { for y in [1] { y } fallback { inner: true } } fallback { outer: true } }`
- **THEN** the result SHALL be `{ outer: true }` (outer fallback triggers because outer for is empty)

#### Scenario: Inner fallback triggers, outer does not
- **WHEN** evaluating `{ for x in [1] { for y in [] { y } fallback { inner: true } } fallback { outer: true } }`
- **THEN** the result SHALL be `{ inner: true }` (inner fallback triggers, outer for yielded)

### Requirement: Fallback does not trigger on errors

When a comprehension's body produces errors on all iterations, the fallback clause SHALL NOT be triggered. Errors SHALL propagate normally.

#### Scenario: Error in body does not trigger fallback
- **WHEN** evaluating `{ for x in [1] { bad: x.nonexistent } fallback { fallbackField: true } }`
- **THEN** the evaluation SHALL report an error, not yield `{ fallbackField: true }`

### Requirement: Fallback with struct embedding

The fallback clause's StructLit SHALL be embedded in the enclosing struct, following the same embedding rules as the main comprehension body.

#### Scenario: Fallback embeds fields
- **WHEN** evaluating `{ existing: 1, if false { added: 2 } else { fallbackField: 3 } }`
- **THEN** the result SHALL be `{ existing: 1, fallbackField: 3 }`

#### Scenario: Fallback can contain multiple fields
- **WHEN** evaluating `{ if false { a: 1 } else { b: 2, c: 3 } }`
- **THEN** the result SHALL be `{ b: 2, c: 3 }`
