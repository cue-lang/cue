[TOC](Readme.md) [Prev](numbers.md) [Next](rangedef.md)

_Types ~~and~~ are Values_

# Bounds

Bounds define a lower bound, upper bound, or inequality for a certain value.
They work on numbers, strings, bytes, and and null.

The bound is defined for all values for which the corresponding comparison
operation is define.
For instance `>5.0` allows all floating point values greater than `5.0`,
whereas `<0` allows all negative numbers (int or float).

<!-- CUE editor -->
_bounds.cue:_
```
rn: >=3 & <8        // type int | float
ri: >=3 & <8 & int  // type int
rf: >=3 & <=8.0     // type float
rs: >="a" & <"mo"

{
    a: rn & 3.5
    b: ri & 3.5
    c: rf & 3
    d: rs & "ma"
    e: rs & "mu"

    r1: rn & >=5 & <10
}
```

<!-- result -->
`$ cue eval -i bounds.cue`
```
a:  3.5
b:  _|_
c:  3.0
d:  "ma"
e:  _|_
r1: >=5 & <8
```
