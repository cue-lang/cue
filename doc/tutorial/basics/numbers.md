[TOC](Readme.md) [Prev](defaults.md) [Next](ranges.md)

_Types and Values_

# Numbers

CUE defines two kinds of numbers.
Integers, denoted `int`, are whole, or integral, numbers.
Floats, denoted `float`, are decimal floating point numbers.

An integer literal (e.g. `4`) can be of either type, but defaults to `int`.
A floating point literal (e.g. `4.0`) is only compatible with `float`.

In the example, the result of `b` is a `float` and cannot be
used as an `int` without conversion.

<!-- CUE editor -->
```
a: int
a: 4 // type int

b: float
b: 4 // type float

c: int
c: 4.0

d: 4  // will evaluate to type int (default)
```

<!-- result -->
```
a: 4
b: 4.0
c: _|_
d: 4
```