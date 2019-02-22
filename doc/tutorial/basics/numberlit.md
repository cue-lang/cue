[TOC](Readme.md) [Prev](comments.md) [Next](stringlit.md)

_JSON Sugar and other Goodness_

# Number Literals


CUE adds a variety of sugar for writing numbers.

<!-- CUE editor -->
_numlit.cue:_
```
[
    1_234,       // 1234
    5M,          // 5_000_000
    1.5Gi,       // 1_610_612_736
    0x1000_0000, // 268_435_456
]
```

<!-- JSON result -->
`$ cue export numlit.cue`
```json
[
    1234,
    5000000,
    1610612736,
    268435456
]
```
