[TOC](Readme.md) [Prev](stringraw.md) [Next](scopes.md)

_JSON Sugar and other Goodness_

# Bytes

CUE distinguishes between a `string` and a `bytes` type.
Bytes are converted to base64 when emitting JSON.
Byte literals are defined with single quotes.
The following additional escape sequences are allowed in byte literals:

    \xnn   // arbitrary byte value defined as a 2-digit hexadecimal number
    \nnn   // arbitrary byte value defined as a 3-digit octal number
<!-- jba: this contradicts the spec, which has \nnn (no leading zero) -->

<!-- CUE editor -->
_bytes.cue:_
```
a: '\x03abc'
```

<!-- result -->
`$ cue export bytes.cue`
```json
{
    "a": "A2FiYw=="
}
```
