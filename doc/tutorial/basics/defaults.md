[TOC](Readme.md) [Prev](disjunctions.md) [Next](disjstruct.md)

_Types ~~and~~ are Values_

# Default Values

If at the time of evaluation a sum type still has more than one possible
value, the first error-free value is taken.
A value is error free if it is not an error, it is a list with only error-free
elements, or it is a struct where all field values are error-free.
The default value must also not be ambiguous.

In the example, `replicas` defaults to `1`.
In the case of `protocol`, however, there are multiple definitions with
different, mutually incompatible defaults.
It is still possible to resolve this error by explicitly setting the value
for protocol.
Try it!
<!-- CUE editor -->
```
// any positive number, 1 is the default
replicas: uint | *1

// the default value is ambiguous
protocol: *"tcp" | "udp"
protocol: *"udp" | "tcp"
```

<!-- result -->
```
replicas: 1
protocol: _|_
```