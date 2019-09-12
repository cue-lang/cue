[TOC](Readme.md) [Prev](sumstruct.md) [Next](ranges.md)

_Types ~~and~~ are Values_

# Numbers

CUE defines two kinds of numbers.
Integers, denoted `int`, are whole, or integral, numbers.
Floats, denoted `float`, are decimal floating point numbers.

An integer literal (e.g. `4`) can be of either type, but defaults to `int`.
A floating point literal (e.g. `4.0`) is only compatible with `float`.

In the example, the result of `b` is a `float` and cannot be
used as an `int` without conversion.

<!-- CUE editor -->
_numbers.cue:_
```
a: int
a: 4 // type int

b: number
b: 4.0 // type float

c: int
c: 4.0

d: 4  // will evaluate to type int (default)
```

<!-- result -->
`$ cue eval -i numbers.cue`
```
a: 4
b: 4.0
c: _|_ // conflicting values int and 4.0 (mismatched types int and float)
d: 4
```
