[TOC](Readme.md) [Prev](interpolation.md) [Next](listcomp.md)

_Expressions_

# Interpolation of Field Names

String interpolations may also be used in field names.

One cannot refer to generated fields with references.

<!-- CUE editor -->
```
sandwich: {
    type:            "Cheese"
    "has\(type)":    true
    hasButter:       true
    butterAndCheese: hasButter && hasCheese
}
```

<!-- result -->
```
sandwich: {
    type:            "Cheese"
    hasCheese:       true
    hasButter:       true
    butterAndCheese: _|_ // unknown reference 'hasCheese'
}
```