[TOC](Readme.md) [Prev](interpolfield.md) [Next](fieldcomp.md)

_Expressions_

# List Comprehensions

Lists can be created with list comprehensions.

The example shows the use of `for` loops and `if` guards.


<!-- CUE editor -->
```
[ x*x for x in items if x rem 2 == 0]

items: [ 1, 2, 3, 4, 5, 6, 7, 8, 9 ]
```

<!-- result -->
```
[4, 16, 36, 64]
```