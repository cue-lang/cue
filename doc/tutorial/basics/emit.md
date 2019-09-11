[TOC](Readme.md) [Prev](aliases.md) [Next](hidden.md)

_References and Visibility_

# Emit Values

By default all top-level fields are emitted when evaluating a configuration.
Embedding a value at top-level will cause that value to be emitted instead.

Emit values allow CUE configurations, like JSON,
to define any type, instead of just structs, while keeping the common case
of defining structs light.

<!-- CUE editor -->
_emit.cue:_
```
"Hello \(who)!"

who: "world"
```

<!-- result -->
`$ cue eval emit.cue`
```
"Hello world!"
```
