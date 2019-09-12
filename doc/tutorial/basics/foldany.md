[TOC](Readme.md) [Prev](fold.md) [Next](comments.md)

_JSON Sugar and other Goodness_

# Folding all Fields

This also works if a struct has more than one member.

In general, any JSON object can be expressed as a collection of
path-leaf pairs without using any curly braces.

<!-- CUE editor -->
_foldany.cue:_
```
outer middle1 inner: 3
outer middle2 inner: 7
```

<!-- result -->
`$ cue export foldany.cue`
```json
{
    "outer": {
        "middle1": {
            "inner": 3
        },
        "middle2": {
            "inner": 7
        }
    }
}
```