[TOC](Readme.md) [Prev](hidden.md) [Next](bottom.md)

_Types and Values_

# Duplicate Fields

CUE allows duplicated field definitions as long as they don't conflict.

For values of basic types this means they must be equal.

For structs, fields are merged and duplicated fields are handled recursively.

For lists, all elements must match accordingly
([we discuss open-ended lists later](lists.md).)

<!-- CUE editor -->
```
a: 4
a: 4

s: {
    x: 1
}
s: {
    y: 2
}

l: [ 1, 2 ]
l: [ 1, 2 ]
```

<!-- result -->
```
a: 4
s: {
    x: 1
    y: 2
}
l: [1, 2]
```