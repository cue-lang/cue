[TOC](Readme.md) [Prev](imports.md) [Next](interpolation.md)

_Expressions_

# Operators

CUE supports many common arithmetic and boolean operators.

The operators for division and remainder are different for `int` and `float`.
For `float` CUE supports the `/` and `%`  operators with the usual meaning.
For `int` CUE supports both Euclidean division (`div` and `mod`)
and truncated division (`quo` and `rem`).

<!-- CUE editor -->
```
a: 3 / 2   // type float
b: 3 div 2 // type int: Euclidean division

c: 3 * "blah"
d: 3 * [1, 2, 3]

e: 8 < 10
```

<!-- result -->
```
a: 1.5
b: 1
c: "blahblahblah"
d: [1, 2, 3, 1, 2, 3, 1, 2, 3]
e: true
```