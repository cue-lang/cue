# Try Expressions

## Purpose

Try expressions enable conditional field inclusion based on whether optional references resolve successfully. The `try` clause allows CUE configurations to gracefully handle missing optional fields without producing errors, with optional fallback via `else` clauses.

This feature is experimental and requires `@experiment(try)` for language version v0.16.0 and later.

## Requirements

### Requirement: Try clause requires experiment flag
The try clause SHALL require the `@experiment(try)` attribute for language versions v0.16.0 and later. Files without this experiment enabled SHALL report a compile error when using try or `?` markers.

#### Scenario: Try used without experiment
- **WHEN** file has no `@experiment(try)` attribute
- **AND** code uses `try { x: a? }`
- **THEN** compile error: "try clause requires the try experiment"

#### Scenario: Optional marker used without experiment
- **WHEN** file has no `@experiment(try)` attribute
- **AND** code uses `a?` anywhere
- **THEN** compile error: "optional marker (?) requires the try experiment"

### Requirement: Optional marker only valid within try context
The `?` marker on references (identifiers, selectors, indices) SHALL only be valid within a try clause body. Using `?` outside of try SHALL produce a compile error.

#### Scenario: Optional marker in try body succeeds
- **WHEN** `@experiment(try)` and `try { x: a? }`
- **THEN** compiles successfully

#### Scenario: Optional marker outside try fails
- **WHEN** `@experiment(try)` and `x: a?` (outside try)
- **THEN** compile error: "optional marker (?) is only valid within a try clause"

#### Scenario: Optional marker in assignment form body fails
- **WHEN** `@experiment(try)` and `try y = a? { x: b? }`
- **THEN** compile error: optional marker (?) is not valid in assignment-form try body

### Requirement: Optional marker tests existence only
The `?` marker SHALL only test whether a field exists, NOT whether its value is concrete. An incomplete value (such as a type constraint like `int`) SHALL be considered to exist and SHALL be returned successfully.

#### Scenario: Incomplete value succeeds
- **WHEN** `incomplete: int` and `try { x: incomplete? } else { fallback: 23 }`
- **THEN** yields `{x: int}` (try succeeds because field exists)

#### Scenario: Defined but incomplete nested value succeeds
- **WHEN** `a: { b: string }` and `try { x: a.b? } else { fallback: "" }`
- **THEN** yields `{x: string}` (try succeeds because field exists)

#### Scenario: Nested struct in try body with incomplete value succeeds
- **WHEN** `a: string` and `try { x: y: a? } else { fallback: "" }`
- **THEN** yields `{x: y: string}` (try succeeds because field exists)

#### Scenario: Undefined optional still triggers else
- **WHEN** `a?: int` (undefined) and `try { x: a? } else { fallback: 23 }`
- **THEN** yields `{fallback: 23}` (field does not exist)

### Requirement: Required fields trigger else when unfilled
A required field (`a!: T`) that has not been given a concrete value SHALL trigger the else clause when referenced with `?`. Required fields are considered "not yet existing" until filled.

#### Scenario: Unfilled required field triggers else
- **WHEN** `a!: _` and `try { x: a? } else { fallback: 23 }`
- **THEN** yields `{fallback: 23}` (required field not yet filled)

#### Scenario: Filled required field succeeds
- **WHEN** `a!: _, a: 5` and `try { x: a? } else { fallback: 23 }`
- **THEN** yields `{x: 5}` (required field has been filled)

#### Scenario: Required field with type constraint triggers else
- **WHEN** `a!: int` and `try { x: a? } else { fallback: 23 }`
- **THEN** yields `{fallback: 23}` (required field not yet filled)

### Requirement: Struct-form try must be last clause
The struct-form try clause (without assignment `try x = expr`) SHALL be the last clause in a comprehension. Placing clauses after struct-form try SHALL produce a compile error.

#### Scenario: Struct-form try as last clause succeeds
- **WHEN** `for x in list try { y: x? }`
- **THEN** compiles successfully

#### Scenario: Struct-form try followed by clause fails
- **WHEN** `try if cond { x: a? }`
- **THEN** compile error: "struct-form try clause must be the last clause in a comprehension"

### Requirement: Try clause evaluates body and yields on success
The try clause SHALL evaluate its body expression. If evaluation succeeds without errors, the try clause SHALL yield the result to the comprehension.

#### Scenario: All optional references resolve
- **WHEN** `a: 1, b: 2` and `try { c: a? + b? }`
- **THEN** the comprehension yields `{c: 3}`

#### Scenario: Non-optional expressions succeed
- **WHEN** `a: 1` and `try { b: a? + 10 }`
- **THEN** the comprehension yields `{b: 11}`

### Requirement: Try clause discards on undefined optional reference
The try clause SHALL discard (not yield) when a `?`-marked reference fails due to an undefined optional field. No error SHALL be reported.

#### Scenario: Single optional reference undefined
- **WHEN** `a?: int` (undefined) and `try { b: a? + 1 }`
- **THEN** the comprehension yields zero results (b is not defined)

#### Scenario: One of multiple optional references undefined
- **WHEN** `a: 1, b?: int` (b undefined) and `try { c: a? + b? }`
- **THEN** the comprehension yields zero results

#### Scenario: Nested optional path undefined
- **WHEN** `x: {}` (x.y undefined) and `try { a: x.y? }`
- **THEN** the comprehension yields zero results

### Requirement: Try clause propagates non-optional errors
The try clause SHALL propagate errors that are NOT from undefined optional references. Type errors, constraint violations, and other failures SHALL surface normally.

#### Scenario: Type error in try body
- **WHEN** `a: "string"` and `try { b: a? + 1 }`
- **THEN** an error is reported (cannot add string and int)

#### Scenario: Constraint violation in try body
- **WHEN** `a: 10` and `try { b: a? & <5 }`
- **THEN** an error is reported (constraint violation)

#### Scenario: Reference without ? to undefined field
- **WHEN** `a?: int` (undefined) and `try { b: a + 1 }`
- **THEN** an error is reported (reference to undefined field without ? is not protected)

#### Scenario: Reference to non-existent field
- **WHEN** `x: {}` and `try { b: x.y? }`
- **THEN** the comprehension yields zero results (y does not exist in closed struct)

#### Scenario: Reference without ? to non-existent field
- **WHEN** `x: {}` and `try { b: x.y }`
- **THEN** an error is reported (field y not found, reference without ? is not protected)

### Requirement: Try clause works with else fallback
The try clause SHALL integrate with the comprehension's else clause. When try yields zero results, the else block SHALL be used.

#### Scenario: Try fails and else provides fallback
- **WHEN** `a?: int` (undefined) and `try { b: a? } else { b: 0 }`
- **THEN** the result is `{b: 0}`

#### Scenario: Try succeeds and else is ignored
- **WHEN** `a: 5` and `try { b: a? } else { b: 0 }`
- **THEN** the result is `{b: 5}`

#### Scenario: Try fails with error and else does not trigger
- **WHEN** `a: "string"` and `try { b: a? + 1 } else { b: 0 }`
- **THEN** an error is reported (type error), else is NOT used as fallback

### Requirement: Nested try clauses scope independently
Each `?`-marked reference SHALL bind to its nearest enclosing try clause. Nested try clauses SHALL manage their optional references independently.

#### Scenario: Inner try fails but outer succeeds
- **WHEN** `a: 1, b?: int` and `try { x: a?, try { y: b? } }`
- **THEN** `x: 1` is yielded, inner try yields nothing (y not defined)

#### Scenario: Both nested tries succeed
- **WHEN** `a: 1, b: 2` and `try { x: a?, try { y: b? } }`
- **THEN** both `x: 1` and `y: 2` are yielded

### Requirement: Assignment form binds expression result
The assignment form `try x = expr { body }` SHALL evaluate `expr`, bind the result to identifier `x`, and make it available in `body`. If `expr` fails due to optional undefined, the entire try SHALL be skipped.

#### Scenario: Assignment with defined value
- **WHEN** `a: 1` and `try x = a? { result: x }`
- **THEN** yields `{result: 1}`

#### Scenario: Assignment with undefined value
- **WHEN** `a?: int` and `try x = a? { result: x } else { fallback: 0 }`
- **THEN** yields `{fallback: 0}`

#### Scenario: Assignment with complex expression
- **WHEN** `a: 1, b: 2` and `try sum = a? + b? { result: sum }`
- **THEN** yields `{result: 3}`

#### Scenario: Chained assignments
- **WHEN** `a: 1, b: 2` and `try x = a? try y = b? { result: x + y }`
- **THEN** yields `{result: 3}`

### Requirement: Closed list index out of range is permanent error
When using `?` with an index expression on a CLOSED list, an out-of-range index SHALL produce a permanent error (not trigger else), because closed lists cannot grow.

#### Scenario: Closed list out of range with else
- **WHEN** `list: [1, 2, 3]` and `try { v: list[10]? } else { fallback: -1 }`
- **THEN** an error is reported (index out of range)

#### Scenario: Closed list in range succeeds
- **WHEN** `list: [1, 2, 3]` and `try { v: list[1]? }`
- **THEN** yields `{v: 2}`

### Requirement: Open list index out of range triggers else
When using `?` with an index expression on an OPEN list, an out-of-range index SHALL trigger OptionalUndefined (and else clause if present), because open lists might grow.

#### Scenario: Open list out of range with else
- **WHEN** `list: [1, 2, 3, ...]` and `try { v: list[10]? } else { fallback: -1 }`
- **THEN** yields `{fallback: -1}`

#### Scenario: Open list out of range without else
- **WHEN** `list: [1, 2, 3, ...]` and `try { v: list[10]? }`
- **THEN** yields `{}` (empty struct)
