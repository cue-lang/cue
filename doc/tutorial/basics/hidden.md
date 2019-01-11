[TOC](Readme.md) [Prev](emit.md) [Next](duplicates.md)

_References and Visibility_

# Hidden Fields

A non-quoted field name that starts with an underscore (`_`) is not
emitted from the output.
To includes fields in the configuration that start with an underscore
put them in quotes.

Quoted an non-quoted fields share the same namespace unless they start
with an underscore.

<!-- CUE editor -->
```
"_foo": 2
_foo:   3
foo:    4
```

<!-- result -->
```
"_foo": 2
foo:    4
```