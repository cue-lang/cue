[TOC](Readme.md) [Prev](numbers.md) [Next](rangedef.md)

_Types and Values_

# Ranges

Ranges define an inclusive range of valid values.
They work on numbers, strings, and bytes.

The type of a range is the unification of the types of the start and end
value.

Unifying two ranges results in the overlapping range or an error if there
is no overlap.

<!-- CUE editor -->
```
rn: 3..5       // type int | float
ri: 3..5 & int // type int
rf: 3..5.0     // type float
rs: "a".."mo"

{
    a: rn & 3.5
    b: ri & 3.5
    c: rf & 3
    d: "ma"
    e: "mu"

    r1: 0..7 & 3..10
}
```

<!-- result -->
```
a:  3.5
b:  _|_
c:  3
d:  "ma"
e:  _|_
r1: 3..7
```