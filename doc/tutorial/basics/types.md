[TOC](Readme.md) [Prev](bottom.md) [Next](unification.md)

_Types ~~and~~ are Values_

# Basic Types

CUE defines the following basic types

```
null bool string bytes int float
```
in addition to the error type mentioned in the previous section.

CUE does not distinguish between types and values.
A field value can be a type (using one of the above names), a concrete value,
or, in case of composite types (lists and structs), anything in between.

In the example, `point` defines an arbitrary point, while `xaxis` and `yaxis`
define the points on the respective lines.
We say that `point`, `xaxis`, and `yaxis` are abstract points, as these
points are underspecified.
Such abstract values cannot be represented as JSON,
which requires all values to be concrete.

The only concrete point is `origin`.
The `origin` is defined to be both on the x-axis and y-axis, which means it
must be at `0, 0`.
Here we see constraints in action:
`origin` evalutes to `0, 0`, even though we did not specify its coordinates
explicitly.


<!-- CUE editor -->
_types.cue:_
```
point: {
    x: number
    y: number
}

xaxis: point
xaxis x: 0

yaxis: point
yaxis y: 0

origin: xaxis & yaxis
```

<!-- result -->
`$ cue eval types.cue`
```
point: {
    x: number
    y: number
}
xaxis: {
    x: 0
    y: number
}
yaxis: {
    x: number
    y: 0
}
origin: {
    x: 0
    y: 0
}
```