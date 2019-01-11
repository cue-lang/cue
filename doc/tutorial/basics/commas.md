[TOC](Readme.md) [Prev](fieldname.md) [Next](commaslists.md)

_JSON Sugar and other Goodness_

# Commas are Optional after Fields

Commas are optional at the end of fields.
This is also true for the last field.
The convention is to omit them.

<!-- Side Note -->
_CUE borrows a trick from Go to achieve this: the formal grammar still
requires commas, but the scanner inserts commas according to a small set
of simple rules._

<!-- CUE editor -->
```
{
    one: 1
    two: 2

    "two-and-a-half": 2.5
}
```


<!-- JSON result -->
```json
{
    "one": 1,
    "two": 2,
    "two-and-a-half": 2.5
}
```