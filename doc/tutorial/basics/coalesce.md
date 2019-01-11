[TOC](Readme.md) [Prev](conditional.md) _Next_

_Expressions_

# Null Coalescing

With null coalescing we really mean error, or bottom, coalescing.
The defaults mechanism for disjunctions can also be
used to provide fallback values in case an expression evaluates to bottom.

In the example the fallback values are specified
for `a` and `b` in case the list index is out of bounds.

<!-- CUE editor -->
```
list: [ "Cat", "Mouse", "Dog" ]

a: list[0] | "None"
b: list[5] | "None"
```

<!-- result -->
```
list: [ "Cat", "Mouse", "Dog" ]

a: "Cat"
b: "None"
```