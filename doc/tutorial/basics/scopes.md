[TOC](Readme.md) [Prev](bytes.md) [Next](selectors.md)

_References and Visibility_

# References and Scopes

A reference refers to the value of the field defined within nearest
enclosing scope.

If no field matches the reference within the file, it may match a top-level
field defined in any other file of the same package.

If there is still no match, it may match a predefined value.

<!-- CUE editor -->
```
v: 1
a: {
    v: 2
    b: v // matches the inner v
}
a: {
    c: v // matches the top-level v
}
b: v
```

<!-- result -->
```
v: 1
a: {
    v: 2
    b: 2
    c: 1
}
b: 1
```
