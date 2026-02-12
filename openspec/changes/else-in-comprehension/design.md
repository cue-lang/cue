## Context

CUE comprehensions use a clause-chaining architecture where each clause (`for`, `if`, `let`) implements a `yield(s *compState)` method. Clauses are evaluated left-to-right, with each clause either:
- Continuing to the next clause via `s.yield(env)` (possibly multiple times for `for`)
- Stopping early (when `if` condition is false, or `for` source is empty)

The final struct is yielded only when all clauses have been traversed. Currently there is no mechanism to emit alternative values when the normal path doesn't yield.

Key files:
- `cue/token/token.go` - Token definitions (no `ELSE` token exists)
- `cue/ast/ast.go` - AST clause types: `ForClause`, `IfClause`, `LetClause`
- `cue/parser/parser.go` - `parseComprehensionClauses()` parses clause sequences
- `internal/core/adt/expr.go` - ADT clause types with `yield()` implementations
- `internal/core/adt/comprehension.go` - Comprehension evaluation via `compState`

## Goals / Non-Goals

**Goals:**
- Add `else` clause syntax that works with both `if` and `for` comprehensions
- For `if`: emit the `else` struct when the condition is false
- For `for`: emit the `else` struct when zero iterations produced results
- Maintain consistency with existing clause architecture
- Support `else` in both struct and list comprehensions

**Non-Goals:**
- Adding `else` to `let` clauses (semantically meaningless)
- Chained `else if` syntax (use nested comprehensions instead)
- Breaking changes to existing comprehension behavior

## Decisions

### 1. Grammar Extension

**Decision:** Add `else` as an optional terminal clause that can follow `if` or appear at the end of a `for`-initiated comprehension.

```
Comprehension       = Clauses StructLit [ ElseClause ] .
Clauses             = StartClause { [ "," ] Clause } .
StartClause         = ForClause | GuardClause .
Clause              = StartClause | LetClause .
ForClause           = "for" identifier [ "," identifier ] "in" Expression .
GuardClause         = "if" Expression .
LetClause           = "let" identifier "=" Expression .
ElseClause          = "else" StructLit .
```

**Rationale:** Making `else` a separate terminal clause (rather than part of `IfClause`) keeps the grammar simple and avoids ambiguity. The `else` always applies to the entire comprehension's yield behavior, not to a single `if` within a multi-clause chain.

**Alternatives considered:**
- `else` as part of `IfClause` like traditional if/else → Creates ambiguity with multi-clause comprehensions (`for x in y if a if b else {...}` - which `if` does `else` attach to?)
- Python-style `for...else` as separate construct → Inconsistent with CUE's unified clause model

### 2. Semantics for `if...else`

**Decision:** When a comprehension has an `if` clause and an `else` clause, the `else` struct is yielded exactly once if and only if the comprehension yields zero values.

```cue
// Example: conditional field emission
{
    if enabled else { default: true }
}
// When enabled=true: {}
// When enabled=false: {default: true}
```

**Rationale:** This provides the expected behavior matching other languages' if/else constructs while fitting CUE's comprehension model where a single comprehension can yield 0-N values.

### 3. Semantics for `for...else`

**Decision:** When a comprehension starts with `for` and has an `else` clause, the `else` struct is yielded exactly once if and only if the `for` loop (including any subsequent filtering `if` clauses) yields zero values.

```cue
// Example: fallback when list is empty
items: [
    for x in list { x * 2 }
    else { 0 }
]
// When list=[1,2]: [2, 4]
// When list=[]: [0]

// Example: fallback when all items filtered
filtered: {
    for k, v in data if v > threshold {
        (k): v
    }
    else { empty: true }
}
// When data has values > threshold: those k:v pairs
// When no values pass filter: {empty: true}
```

**Rationale:** This is more useful than Python's `for...else` (which triggers when no `break` occurs). In a declarative language without `break`, "no results" is the natural trigger condition.

### 4. Implementation Architecture

**Decision:** Track yield count in `compState` and invoke else clause after main evaluation.

```go
type compState struct {
    ctx       *OpContext
    comp      *Comprehension
    i         int
    f         YieldFunc
    state     vertexStatus
    yieldCount int  // NEW: track number of yields
}

func (s *compState) yield(env *Environment) (ok bool) {
    // ... existing logic ...
    s.f(env)
    s.yieldCount++  // NEW: increment on each yield
    return true
}
```

After all clauses complete, if `yieldCount == 0` and an `ElseClause` exists, evaluate and yield the else struct.

**Rationale:** Minimal change to existing architecture. The `compState` already tracks clause evaluation state; adding a yield counter is straightforward and doesn't change the evaluation model.

**Alternatives considered:**
- Wrapping the entire comprehension in a tracking layer → More invasive, requires restructuring
- Using a separate "else mode" evaluation pass → Inefficient, evaluates clauses twice

### 5. Scoping in Else Clause

**Decision:** The `else` clause has access to the enclosing scope but NOT to variables bound by `for` or `let` within the comprehension.

```cue
outer: 1
{
    for x in empty let y = x {
        result: y
    }
    else {
        fallback: outer  // OK: outer scope visible
        // x and y are NOT visible here
    }
}
```

**Rationale:** Since `else` triggers when no iterations succeed, the `for`/`let` variables have no meaningful values. This matches the mental model: `else` is an alternative path, not a continuation.

### 6. Multiple Else Clauses

**Decision:** A comprehension may have at most one `else` clause. Multiple `else` clauses are a parse error.

**Rationale:** Multiple `else` clauses would be confusing (which one triggers?) and provide no benefit over nesting.

## Risks / Trade-offs

**Risk: Semantic confusion with multi-clause comprehensions**
→ Mitigation: Document clearly that `else` applies to the entire comprehension's output, not individual clauses. Provide examples showing behavior with `for x if a if b else`.

**Risk: Performance overhead from yield counting**
→ Mitigation: A single integer increment per yield is negligible. Only comprehensions with `else` clauses pay the cost.

**Risk: Breaking existing code that uses `else` as identifier**
→ Mitigation: `else` is already a reserved word in CUE (reserved for future use). No existing valid CUE code uses `else` as an identifier.

**Trade-off: `else` applies to whole comprehension vs. individual `if`**
→ Chose whole-comprehension semantics for simplicity. Users wanting per-`if` else behavior can nest comprehensions:
```cue
for x in list {
    if cond {
        result: x
    } else {
        fallback: x
    }
}
```

## Open Questions

1. **Interaction with disjunctions:** If a comprehension body contains disjunctions, how does `else` interact? Proposed: `else` triggers only if zero values are yielded, regardless of disjunction expansion.

2. **Error handling:** If the comprehension body errors on all iterations, should `else` trigger? Proposed: No, errors propagate normally; `else` is for "no results" not "all results failed".

3. **`else if` sugar:** Should we support `else if cond { }` as sugar for `else { if cond { } }`? Proposed: Defer to future enhancement; nesting works for now.
