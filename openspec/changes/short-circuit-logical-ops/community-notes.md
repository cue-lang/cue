# Short-Circuit `&&` and `||` in CUE v0.17.0

CUE v0.17.0 introduces an experiment that makes the `&&` and `||` operators
truly short-circuiting, matching the behavior already described in the CUE
specification ("the right operand is evaluated conditionally") and consistent
with every other language that has these operators.

## What changed

Without `@experiment(shortcircuit)`, both operands of `&&` and `||` are always
evaluated. This means errors or incomplete values on the right side propagate
even when the result is already determined:

```cue
// Today (no experiment): false && _|_ yields an error
x: false && _|_  // _|_  ← error propagates
```

With `@experiment(shortcircuit)`, the right operand is only evaluated when
needed:

```cue
@experiment(shortcircuit)

x: false && _|_  // false  ← right not evaluated; error suppressed
y: true  || _|_  // true   ← right not evaluated; error suppressed
```

## How to opt in

Add `@experiment(shortcircuit)` at the very top of any `.cue` file
(before field declarations):

```cue
@experiment(shortcircuit)

// Your file content here
```

The experiment is available from language version v0.17.0. If your module
file uses an older version, the attribute will be rejected with an error.

## Simulating the old behavior

If you have code that deliberately relies on the right operand being evaluated
even when it would be short-circuited, use an intermediate field to force
evaluation before the conditional:

```cue
// Old behavior (both operands evaluated):
//   if X && Y {}
//
// Equivalent that forces Y to be evaluated regardless of X:
_y: Y
if X && _y {}
```

Similarly for `||`:

```cue
// Old behavior:
//   if X || Y {}
//
// Equivalent that forces Y to be evaluated regardless of X:
_y: Y
if X || _y {}
```

Note: the CUE builtins `and([...])` and `or([...])` are CUE-value unification
(`&`) and disjunction (`|`) over a list — they are **not** logical conjunction
and disjunction and cannot be used as drop-in replacements.

## Why this change

The operators `&&` and `||` are short-circuiting in every mainstream language
(Go, Python, JavaScript, Rust, ...). The CUE spec always described them this
way. The previous implementation was a bug. Short-circuit semantics also make
`&&` and `||` genuinely useful for guarding against incomplete values in `if`
conditions and comprehensions.
