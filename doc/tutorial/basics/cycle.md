[TOC](Readme.md) [Prev](coalesce.md) [Next](cycleref.md)

_Cycles_

# Reference cycles

CUE can handle many types of cycles just fine.
Because all values are final, a field with a concrete value of, say `200`,
can only be valid if it is that value.
So if it is unified with another expression, we can delay the evaluation of
this until later.

By postponing that evaluation, we can often break cycles.
This is very useful for template writers that may not know what fields
a user will want ot fill out.


<!-- CUE editor -->
```
// CUE knows how to resolve the following:
x: 200
x: y + 100
y: x - 100

// If a cycle is not broken, CUE will just report it.
a: b + 100
b: a - 100
```

<!-- result -->
```
x: 200
y: 100

a: _|_ // cycle detected
b: _|_ // cycle detected
```
