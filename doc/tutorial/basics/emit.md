[TOC](Readme.md) [Prev](aliases.md) [Next](hidden.md)

_References and Visibility_

# Emit Values

By default all top-level fields are emitted when evaluating a configuration.
CUE files may define a top-level value that is emitted instead.
<!-- jba:
It's unclear how they do that. Is it the first form in the file?
And this is not in the spec AFAICT.
-->

Values within the emit value may refer to fields defined outside of it.

Emit values allow CUE configurations, like JSON,
to define any type, instead of just structs, while keeping the common case
of defining structs light.

<!-- CUE editor -->
_emit.cue:_
```
{
    a: A
    b: B
}

A: 1
B: 2
```

<!-- result -->
`$ cue eval emit.cue`
```
a: 1
b: 2
```
