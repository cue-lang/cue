[TOC](Readme.md) [Prev](commas.md) [Next](curly.md)

_JSON Sugar and other Goodness_

# Commas are Still Required in Lists


Commas are still required as separators in lists.
The last element of a list may also have a comma.

<!-- CUE editor -->
_commas2.cue:_
```
[
    1,
    2,
    3,
]
```

<!-- JSON result -->
`$ cue export commas2.cue`
```json
[
    1,
    2,
    3
]
```