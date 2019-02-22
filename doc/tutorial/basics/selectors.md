[TOC](Readme.md) [Prev](scopes.md) [Next](aliases.md)

_References and Visibility_

# Accessing Fields

Selectors access fields within a struct using the `.` notation.
This only works if a field name is a valid identifier and it is not computed.
For other cases one can use the indexing notation.


<!-- CUE editor -->
_selectors.cue:_
```
a: {
    b: 2
    "c-e": 5
}
v: a.b
w: a["c-e"]
```

<!-- result -->
`$ cue eval selectors.cue`
```
a: {
    b:     2
    "c-e": 5
}
v: 2
w: 5
```
