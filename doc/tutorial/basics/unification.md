[TOC](Readme.md) [Prev](types.md) [Next](disjunctions.md)

_Types and Values_

# Order is Irrelevant

As mentioned before, values of duplicates fields are combined.
This process is called unification.
Unification can also be written explicitly with the `&` operator.

There is always a single unique result, possibly bottom,
for unifying any two CUE values.

Unification is commutative, associative, and idempotent.
In other words, order doesn't matter and unifying a given set of values
in any order always gives the same result.

<!-- CUE editor -->
```
a: { x: 1, y: 2 }
b: { y: 2, z: 3 }
c: { x: 1, z: 4 }

q: a & b & c
r: b & c & a
s: c & b & a
```

<!-- result -->
```
a: { x: 1, y: 2 }
b: { y: 2, z: 3 }
c: { x: 1, z: 4 }

q: { x: 1, y: 2, z: _|_ }
r: { x: 1, y: 2, z: _|_ }
s: { x: 1, y: 2, z: _|_ }
```