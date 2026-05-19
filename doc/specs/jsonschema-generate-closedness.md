## JSON Schema Generation: Closedness and References

### The Two Bugs

**#4352 — Closedness lost in disjunctions**: `#S: int | {a: int}`
generates the struct disjunct without `additionalProperties: false`.
Root cause: `generate.go:401-404` discards the closedness mode when
creating disjunction elements, always passing `open`:

```go
case cue.OrOp:
    return &itemAnyOf{
        elems: mapSlice(args, func(v cue.Value) internItem { return g.makeItem(v, open) }),
    }
```

This is a straightforward bug — `mode` should be passed instead of
`open`.

**#4356 — `additionalProperties: false` at root/embedding level with
`$ref`**: When the root value is `#T` (a definition reference), the
output includes both `additionalProperties: false` and `$ref:
"#/$defs/T"` in the same schema object. Since JSON Schema's
`additionalProperties` only considers `properties` in the *same* schema
object, and the root has no `properties` keyword, this rejects ALL
properties — even those defined inside `#T`.

### The Fundamental Semantic Mismatch

In CUE, closedness applies to the *unified result* of all conjuncts,
including embedded definitions. When `#S: {s?: int, #T}` and `#T: {t?:
int}`, the closed struct `#S` allows exactly `{s, t}`.

In JSON Schema, `additionalProperties` is lexically scoped — it only
sees `properties` defined in the *same schema object*. Properties
introduced via `$ref` are invisible to it. So this output is broken:

```json
{
    "additionalProperties": false,
    "properties": {"s": {"type": "integer"}},
    "$ref": "#/$defs/T"
}
```

The `t` property from `#T` is rejected because it's not in the local
`properties`.

This mismatch affects two distinct cases:

1. **Root-level definition reference** (`#T` embedded at package level):
   The root gets `additionalProperties: false` + `$ref` with no local
   `properties`.

2. **Definition embedding** (`#S: {s?: int, #T}`): `#S` gets
   `additionalProperties: false` + local `properties: {s}` + `$ref:
   "#/$defs/T"`, but `t` isn't in local `properties`.

### The Non-Definition Reference Problem

A non-definition like `_T` can be referenced from both inside and
outside a definition:

```cue
_T: {a?: int}
#D: _T     // closed context — needs additionalProperties: false
x?: _T     // open context — no additionalProperties
```

A single `$defs` entry for `_T` can't serve both contexts. Currently the
code creates all definitions with `defMode = open` for non-definitions
and `defMode = closedRecursively` for definitions
(`generate.go:379-382`). Non-definitions always get open definitions,
which is correct for `x?: _T` but means `#D: _T` loses closedness
(though in practice the closedness comes from `#D` being a definition).

### `unevaluatedProperties` — Why Not

JSON Schema 2020-12 provides `unevaluatedProperties`, which *can* see
through `$ref`, `allOf`, `anyOf`, etc. Using `unevaluatedProperties:
false` instead of `additionalProperties: false` would fix the immediate
bugs. However:

1) **CUE can't consume what it generates.** The CUE jsonschema *decoder*
   doesn't support `unevaluatedProperties`. Generating a keyword we
   can't ourselves interpret creates an asymmetry.

2) **Semantic mismatch with disjunctions.** `unevaluatedProperties`
   considers a property "evaluated" if *any* branch of an `anyOf`
   evaluates it — even branches that don't match. It also considers
   properties evaluated by `not` schemas. CUE's closed-struct semantics
   are simpler and don't have these edge cases. This means
   `unevaluatedProperties: false` would technically have different
   semantics from CUE closedness in corner cases involving disjunctions.

3) **Validator support.** While Draft 2020-12 validators should support
   it, `unevaluatedProperties` is one of the more complex,
   less-implemented, and less-used keywords.

### Proposed Solutions

#### Fix 1: Propagate mode through disjunctions (fixes #4352)

Change `generate.go:401-404`:

```go
case cue.OrOp:
    return &itemAnyOf{
        elems: mapSlice(args, func(v cue.Value) internItem { return g.makeItem(v, mode) }),
    }
```

This is low-risk — it simply stops dropping information that was
already available.

#### Fix 2: Don't emit struct constraints at root when root is purely a reference (partially fixes #4356)

When the root value's expression is just `AndOp(#T, {})` (definition
reference + empty package struct), the root should generate only `$ref:
"#/$defs/T"` without `type`, `additionalProperties`, or `properties`.
The closedness is already encoded in the definition.

In `makeStructItem`, when a closed struct's only content is reference
conjuncts and the non-reference conjuncts contribute no fields, the
struct-level `additionalProperties: false` is redundant with the
definition's own closedness. We can detect this and omit it.

More precisely: when `mode != open` and the `props.properties` map is
empty and `allOf` only contains reference items, don't add
`additionalProperties: false`.

#### Fix 3: Lift embedded properties for the general embedding case (fixes #4356 fully)

For the general case (`#S: {s?: int, #T}`), we need the local
`properties` to include ALL fields — including those from embedded
definitions — so that `additionalProperties: false` works correctly.

**Approach A — Property lifting with `true`**: When `makeStructItem`
encounters a reference conjunct in a closed context, extract the
referenced value's field names and add them to `properties` with
constraint `true`. The actual constraint comes from the `$ref`:

```json
{
    "additionalProperties": false,
    "properties": {"s": {"type": "integer"}, "t": true},
    "$ref": "#/$defs/T",
    "required": ["s", "t"]
}
```

Implementation sketch: in `makeStructItem`, when we encounter a
reference conjunct (`pkg.Exists()`), if `mode != open`, iterate the
referenced value's fields and add property entries with `true` for any
field not already in `properties`. This interacts naturally with the
existing code — the reference still produces a `$ref`, and the lifted
properties just inform `additionalProperties` about allowed field names.

**Approach B — Full inlining for embeddings**: When a closed struct
embeds a definition, don't use `$ref` — inline all the definition's
properties into the parent struct. Only use `$ref` for field-value
references (`x?: #T`), not for embeddings.

This avoids the `additionalProperties` / `$ref` conflict entirely, but
loses the reference semantics for embeddings. The schema consumer can't
see that `t` came from `#T`.

**Approach C — Selective inlining for non-definitions**:
Non-definition references that appear in closed contexts could be
inlined rather than referenced, avoiding the need for dual definitions.
Definition references would continue using `$ref` (their closedness is
inherent). If a cycle is detected during inlining, fall back to creating
a `$defs` entry.

Implementation: maintain a map per reference tracking whether it's been
used in a closed context, an open context, or both. On first use, defer
the decision. On second use in a conflicting context, create two
definitions. For non-recursive references, inline the
conflicting-context use.

### Recommended Path

1. **Fix #4352** immediately — propagate mode through `OrOp`. This is
   a clear bug with a one-line fix.

2. **Fix the root case of #4356** — detect when the root struct's only
   non-trivial content is a `$ref` and omit redundant
   `additionalProperties: false`. This is low-risk.

3. **Fix the embedding case of #4356** with Approach A (property
   lifting) — when a closed struct contains `$ref` from an embedded
   definition, lift the referenced definition's property names (with
   `true`) into the local `properties`. This preserves both `$ref`
   semantics and correct closedness.

4. **Later**: consider whether non-definition references need a
   dual-definition mechanism, and whether `unevaluatedProperties`
   support in the decoder would unlock using it in the encoder.

### Inherent Limitations

1) **No perfect JSON Schema equivalent for CUE embedding + closedness.**
   CUE's model of "closed struct that merges fields from embedded
   definitions" has no direct JSON Schema counterpart. Property lifting
   is a faithful approximation but introduces `true` property entries
   that look odd (though they're semantically correct).

2) **A single `$defs` entry can't serve both open and closed contexts.**
   This is fundamental to JSON Schema's design. If a value is referenced
   from both, we must either inline, duplicate, or accept that one
   context will be imprecise.

3) **Recursive non-definitions can't be inlined.** Mutual recursion
   forces `$defs` entries, and if those entries need to serve
   conflicting closedness contexts, there's no perfect solution without
   `unevaluatedProperties` or definition duplication.

4) **`unevaluatedProperties` semantics don't perfectly match CUE
   closedness.** Even if we adopted `unevaluatedProperties`, its
   "evaluated" concept (which includes branches of `anyOf` that didn't
   match) diverges from CUE's closed-struct semantics. Round-tripping
   would not be exact.
