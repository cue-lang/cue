[TOC](Readme.md) [Prev](emit.md) [Next](duplicates.md)

_References and Visibility_

# Hidden Fields

A non-quoted field name that starts with an underscore (`_`) is not
emitted from the output.
To include fields in the configuration that start with an underscore
put them in quotes.

Quoted and non-quoted fields share the same namespace unless they start
with an underscore.

<!-- CUE editor -->
_hidden.cue:_
```
"_foo": 2
_foo:   3
foo:    4
```

<!-- result -->
`$ cue export hidden.cue`
```
{
    "_foo": 2,
    "foo": 4
}
```
