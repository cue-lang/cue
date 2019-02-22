[TOC](Readme.md) [Prev](cycle.md) _Next_

_Cycles_

# Cycles in fields

Also, we know that unifying a field with itself will result in the same value.
Thus if we have a cycle between some fields, all we need to do is ignore
the cycle and unify their values once to achieve the same result as
merging them ad infinitum.

<!-- CUE editor -->
_cycleref.cue:_
```
labels: selectors
labels: {app: "foo"}

selectors: labels
selectors: {name: "bar"}
```

<!-- result -->
`$ cue eval cycleref.cue`
```
labels:    {app: "foo", name: "bar"}
selectors: {app: "foo", name: "bar"}
```
