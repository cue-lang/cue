[TOC](Readme.md) [Prev](curly.md) [Next](foldany.md)

# Folding of Single-Field Structs

CUE allows a shorthand for structs with single members.

<!-- CUE editor -->
```
outer middle inner: 3
```

<!-- JSON result -->
```json
"outer": {
    "middle": {
        "inner": 3
    }
}
```