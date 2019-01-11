[TOC](Readme.md) [Prev](conditional.md) _Next_

_Expressions_

# Null Coalescing

With null coalescing we really mean error, or bottom, coalescing.
The defaults mechanism for disjunctions can also be
used to provide fallback values in case an expression evaluates to bottom.

In the example the fallback values are specified
for `a` and `b` in case the list index is out of bounds.

To do actual null coalescing one can unify a result with the desired type
to force an error.
In that case the default will be used if either the lookup fails or
the result is not of the desired type.

<!-- CUE editor -->
```
list: [ "Cat", "Mouse", "Dog" ]

a: list[0] | "None"
b: list[5] | "None"

n: [null]
v: n[0] & string | "default"
```

<!-- result -->
```
list: [ "Cat", "Mouse", "Dog" ]

a: "Cat"
b: "None"
n: [null]
v: "default"
```