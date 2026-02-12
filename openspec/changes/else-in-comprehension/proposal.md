## Why

CUE comprehensions currently have no way to specify alternative values when conditions are not met or iterations produce no results. Users must resort to workarounds like `if cond { a } | if !cond { b }` for conditional defaults, or use separate logic to detect empty iteration results. Adding `else` to comprehensions provides a natural, readable way to express fallback behavior that aligns with expectations from other languages.

## What Changes

- **New `else` clause for `if` comprehensions**: When an `if` clause condition evaluates to false, the `else` clause's struct is yielded instead. This enables concise conditional field emission.

- **New `else` clause for `for` comprehensions**: When a `for` loop iterates but yields zero values (either due to an empty source or all values being filtered by subsequent `if` clauses), the `else` clause's struct is yielded. This is similar to Python's `for...else` but triggers on "no results" rather than "no break".

- **Grammar extension**: The `else` keyword becomes valid within comprehension clause sequences, following `if` or at the end of a `for`-initiated comprehension.

- **Token addition**: The `else` keyword is added to CUE's token set.

## Capabilities

### New Capabilities

- `comprehension-else`: Defines the syntax, semantics, and behavior of the `else` clause in both `if` and `for` comprehensions, including clause ordering rules, scoping, and interaction with existing clauses.

### Modified Capabilities

(none - this is an additive feature with no changes to existing spec requirements)

## Impact

**Parser & Lexer**:
- `cue/token/token.go`: Add `ELSE` token
- `cue/ast/ast.go`: Add `ElseClause` AST node
- `cue/parser/parser.go`: Extend `parseComprehensionClauses()` to handle `else`

**Compiler**:
- `internal/core/compile/compile.go`: Compile `ast.ElseClause` to `adt.ElseClause`

**Evaluator**:
- `internal/core/adt/expr.go`: Add `ElseClause` ADT type implementing `Yielder`
- `internal/core/adt/comprehension.go`: Modify comprehension evaluation to track yield counts and trigger else

**Documentation**:
- `doc/ref/spec.md`: Update grammar and comprehension semantics

**Testing**:
- New test cases for `if...else` and `for...else` comprehensions
- Edge cases: nested comprehensions with else, multiple else clauses (error), else with let bindings
