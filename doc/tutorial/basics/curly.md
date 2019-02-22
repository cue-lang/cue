[TOC](Readme.md) [Prev](commaslists.md) [Next](fold.md)

_JSON Sugar and other Goodness_

# Curly Braces

The outer curly braces may be omitted for the top-level struct.
CUE also allows both, which has a specific meaning.
[We will come back to that later](emit.md).

<!-- CUE editor -->
_curly.cue:_
```
one: 1
two: 2

"two-and-a-half": 2.5
```

<!-- JSON result -->
`$ cue export curly.cue`
```json
{
    "one": 1,
    "two": 2,
    "two-and-a-half": 2.5
}
```

