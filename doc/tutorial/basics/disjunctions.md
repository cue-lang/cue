[TOC](Readme.md) [Prev](unification.md) [Next](defaults.md)

_Types ~~and~~ are Values_

# Disjunctions

Disjunctions, or sum types, define a new type that is one of several things.

In the example, `conn` defines a `protocol` field that must be one of two
values: `"tcp"` or `"udp"`.
It is an error for a concrete `conn`
to define anything else than these two values.

<!-- CUE editor -->
_disjunctions.cue_
```
conn: {
    address:  string
    port:     int
    protocol: "tcp" | "udp"
}

lossy: conn & {
    address:  "1.2.3.4"
    port:     8888
    protocol: "udp"
}
```
