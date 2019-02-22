[TOC](Readme.md) [Prev](rangedef.md) [Next](lists.md)

_Expressions_

# Regular expressions

The `=~` and `!~` operators can be used to check against regular expressions.

The expression `a =~ b` is true if `a` matches `b`, while
`a !~ b` is true if `a` does _not_ match `b`.

Just as with comparison operators, these operators maybe be used
as unary versions to define a set of strings.


<!-- CUE editor -->
_regexp.cue:_
```
a: "foo bar" =~ "foo [a-z]{3}"
b: "maze" !~ "^[a-z]{3}$"

c: =~"^[a-z]{3}$" // any string with lowercase ASCII of length 3

d: c
d: "foo"

e: c
e: "foo bar"
```

<!-- result -->
`$ cue eval -i regexp.cue`
```
a: true
b: true

c: "^[a-z]{3}$"

d: "foo"
e: _|_  // "foo bar" does not match =~"^[a-z]{3}$"
```