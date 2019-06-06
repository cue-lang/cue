[TOC](Readme.md) [Prev](duplicates.md) [Next](types.md)

_Types ~~and~~ are Values_

# Bottom

Specifying duplicate fields with conflicting values results in an error
or bottom.
_Bottom_ is a special value in CUE, denoted `_|_`, that indicates an
error such as incompatible values.
Any error in CUE results in `_|_`.
Logically all errors are equal, although errors may be associated with
metadata such as an error message.

Note that an error is different from `null`: `null` is a valid value,
whereas `_|_` is not.

<!-- CUE editor -->
_bottom.cue:_
```
a: 4
a: 5

l: [ 1, 2 ]
l: [ 1, 3 ]

list: [0, 1, 2]
val: list[3]
```

<!-- result -->
`$ cue eval -i bottom.cue`
```
a: _|_ /* conflicting values: 4 != 5 */
l: [1, _|_ /* conflicting values: 2 != 3 */]
list: [0, 1, 2]
val: _|_ /* index 3 out of bounds */
```
