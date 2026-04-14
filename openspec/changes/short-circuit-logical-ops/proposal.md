## Why

`&&` and `||` are already documented in the CUE spec as short-circuiting ("The
right operand is evaluated conditionally"), but the implementation evaluates
both operands unconditionally. This means errors or incomplete values on the
right-hand side propagate even when short-circuit semantics would suppress them.
All mainstream languages short-circuit these operators; without it, users must
use `&` or `|` workarounds.

## What Changes

- `&&` becomes truly short-circuiting: when the left operand evaluates to
  `false`, the right operand is **not** evaluated and the result is `false`.
- `||` becomes truly short-circuiting: when the left operand evaluates to
  `true`, the right operand is **not** evaluated and the result is `true`.
- Two new predeclared built-in functions, `all(list)` and `some(list)`, provide
  strict (non-short-circuiting) logical conjunction and disjunction over a
  boolean list. Unlike `&&`/`||`, they evaluate every element: any error or
  incomplete value causes an error.
- All three features are gated behind a per-file `@experiment(shortcircuit)`
  attribute initially, with `preview:v0.17.0`.
- No parser, scanner, or AST changes required.

## Capabilities

### New Capabilities

- `short-circuit-logical-ops`: Short-circuit evaluation of `&&` and `||`
  operators, where the right operand is only evaluated when the left operand
  does not determine the result.
- `all-some-builtins`: New predeclared builtins `all(list)` and `some(list)`
  for strict logical AND and OR over boolean lists (non-short-circuiting
  counterparts to `&&` and `||`).

### Modified Capabilities

_(none — this is a bug fix / spec compliance change, not new public API surface)_

## `all` and `some` built-in specification

### `all(list) bool`

Evaluates every element of `list`. Returns `true` if all elements are `true`,
`false` if any element is `false`. Any error or incomplete element causes an
error, even if the boolean result is already determined.

- Empty list → `true` (vacuous truth)
- Any non-bool concrete element → error: "non-bool value in call to all"
- Any error or incomplete element → error (propagates unconditionally)

### `some(list) bool`

Evaluates every element of `list`. Returns `true` if any element is `true`,
`false` if all elements are `false`. Any error or incomplete element causes an
error, even if the boolean result is already determined.

- Empty list → `false`
- Any non-bool concrete element → error: "non-bool value in call to some"
- Any error or incomplete element → error (propagates unconditionally)

### Relationship to `&&`/`||` and to `and`/`or`

`all` and `some` are the **strict** (non-short-circuiting) counterparts of
`&&` and `||`. Where `false && X` suppresses any error in `X`, `all([false, X])`
surfaces it.

`and` and `or` apply CUE's `&` and `|` operators over **arbitrary CUE values**:
`and([a, b]) = a & b`. They are **not** logical operators. In particular,
`and([true, false]) = true & false = _|_` (a conflict), not `false`.

`all` and `some` operate exclusively on booleans and return a boolean result.
They cannot be used as a drop-in replacement for `and`/`or` or vice versa.

### Strict error propagation with incomplete values

| Expression                        | Result        |
|-----------------------------------|---------------|
| `all([false, incompleteBool])`    | error         |
| `all([true, incompleteBool])`     | error         |
| `some([true, incompleteBool])`    | error         |
| `some([false, incompleteBool])`   | error         |

Contrast with `&&`/`||`: `false && X` returns `false` regardless of `X`.

## Impact

- **`internal/core/adt/expr.go`**: `BinaryExpr.evaluate()` needs a special case
  for `BoolAndOp` and `BoolOrOp` that evaluates the left operand first, then
  conditionally evaluates the right.
- **`internal/cueexperiment/file.go`**: Add `ShortCircuit bool` field
  with `experiment:"preview:v0.17.0"`.
- **`internal/core/compile/builtin.go`**: Add `allBuiltin` and `someBuiltin`
  predeclared built-ins, gated on `call.Pos().Experiment().ShortCircuit`.
- **`internal/core/compile/predeclared.go`**: Register `all`/`__all` and
  `some`/`__some` in the `predeclared` switch.
- **`doc/ref/spec.md`**: Document `all`, `some`, and the shortcircuit experiment.
- **Test data**: txtar tests covering short-circuit, `all`/`some` semantics, and
  the no-experiment case.
- **No API changes** — pure evaluation semantics change, transparent to callers.
- **Potential behavior change**: existing CUE programs that rely on right-operand
  side effects or error propagation under `&&`/`||` will observe different
  behavior — this is intentional and correct per the spec.
