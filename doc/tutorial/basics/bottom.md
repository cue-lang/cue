[TOC](Readme.md) [Prev](duplicates.md) [Next](types.md)

_Types and Values_

# Bottom

Specifying duplicate fields with conflicting values results in an error,
denoted `_|_`.

Technically speaking, bottom is just a value like any other.
But for all practical purposes it is okay to think of the bottom value
as an error.

Note that an error is different from `null`: `null` is a valid JSON value,
whereas `_|_` is not.

<!-- CUE editor -->
```
a: 4
a: 5

l: [ 1, 2 ]
l: [ 1, 3 ]
```

<!-- result -->
```
a: _|_
l: _|_
```