[TOC](Readme.md) [Prev](json.md) [Next](commas.md)

_JSON Sugar and other Goodness_

# Quotes are Optional for Field Names

JSON objects are called structs in CUE.
An object member is called a field.


Double quotes may be omitted from field names if their name contains no
special characters and does not start with a number:

<!-- CUE editor -->
```
{
    one: 1,
    two: 2,

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