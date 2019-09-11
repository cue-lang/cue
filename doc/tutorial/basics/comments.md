[TOC](Readme.md) [Prev](foldany.md) [Next](numberlit.md)

_JSON Sugar and other Goodness_

# Comments

CUE supports C-style line comments.

<!-- CUE editor -->
_comments.cue:_
```
// a doc comment
one: 1
two: 2 // a line comment
```

<!-- JSON result -->
`$ cue export comments.cue`
```json
{
    "one": 1,
    "two": 2,
}
```