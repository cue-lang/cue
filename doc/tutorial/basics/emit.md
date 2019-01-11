[TOC](Readme.md) [Prev](aliases.md) [Next](hidden.md)

_References and Visibility_

# Emit Values

By default all top-level fields are emitted when evaluating a configuration.
CUE files may define a top-level value that is emitted instead.

Values within the emit value may refer to fields defined outside of it.

Emit values allow CUE configurations, like JSON,
to define any type, instead of just structs, while keeping the common case
of defining structs light.

<!-- CUE editor -->
```
{
    a: A
    b: B
}

A: 1
B: 2
```

<!-- result -->
```
a: 1
b: 2
```