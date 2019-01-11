[TOC](Readme.md) [Prev](rangedef.md) [Next](templates.md)

_Types ~~and~~ are Values_

# Lists

Lists define arbitrary sequences of CUE values.
A list can be closed or open ended.
Open-ended lists may have some predefined elements, but may have
additional, possibly typed elements.

In the example we define `IP` to be a list of `4` elements of type `uint8`, which
is a predeclared value of `0..255`.
`PrivateIP` defines the IP ranges defined for private use.
Note that as it is already defined to be an `IP`, the length of the list
is already fixed at `4` and we do not have to specify a value for all elements.
Also note that instead of writing `...uint8`, we could have written `...`
as the type constraint is already already implied by `IP`.

The output contains a valid private IP address (`myIP`)
and an invalid one (`yourIP`).

<!-- CUE editor -->
```
IP: 4 * [ uint8 ]

PrivateIP: IP
PrivateIP: [10, ...uint8] | [192, 168, ...] | [172, 16..32, ...]

myIP: PrivateIP
myIP: [10, 2, 3, 4]

yourIP: PrivateIP
yourIP: [11, 1, 2, 3]
```

<!-- result -->
```
IP: [0..255, 0..255, 0..255, 0..255]
PrivateIP:
    [10, 0..255, 0..255, 0..255] |
    [192, 168, 0..255, 0..255] |
    [172, 16..32, 0..255, 0..255]

myIP:   [10, 2, 3, 4]
yourIP: _|_
```