[TOC](Readme.md) [Prev](listcomp.md) [Next](conditional.md)

_Expressions_

# Field Comprehensions

CUE also supports comprehensions for fields.

One cannot refer to generated fields with references.
Instead, one must use indexing.

<!-- CUE editor -->
```
import "strings"

a: [ "Barcelona", "Shanghai", "Munich" ]

{
    "\( strings.ToLower(v) )": {
        pos:     k + 1
        name:    v
        nameLen: len(v)
    } for k, v in a
}
```

<!-- result -->
```
barcelona: {
    pos:     1
    name:    "Barcelona"
    nameLen: 9
}
shanghai: {
    pos:     2
    name:    "Shanghai"
    nameLen: 9
}
munich: {
    pos:     3
    name:    "Munich"
    nameLen: 9
}
```