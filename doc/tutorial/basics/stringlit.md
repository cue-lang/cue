[TOC](Readme.md) [Prev](numberlit.md) [Next](bytes.md)

_JSON Sugar and other Goodness_

# String Literals

CUE strings allow a richer set of escape sequences than JSON.

CUE also supports multi-line strings, enclosed by a pair of triple quotes `"""`.
The opening quote must be followed by a newline.
The closing quote must also be on a newline.
The whitespace directly preceding the closing quote must match the preceding
whitespace on all other lines and is removed from these lines.

Strings may also contain [interpolations](interpolation.md).

<!-- CUE editor -->
```
// 21-bit unicode characters
a: "\U0001F60E" // 😎

// multiline strings
b: """
    Hello
    World!
    """
```

<!-- JSON result -->
```json
{
    a: "😎"
    b: "Hello\nWorld!"
}
```
