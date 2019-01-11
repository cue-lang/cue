[TOC](Readme.md) [Prev](lists.md) [Next](instances.md)

_Types ~~and~~ are Values_

# Templates

One of CUE's most powerful features is templates.
A template defines a value to be unified which each field of a struct.

The template's identifier (in angular brackets) is bound to name of each
of its sibling fields and is visible within the template value
that is unified with each of the siblings.

```
// The following struct is unified with all elements in job.
// The name of each element is bound to Name and visible in the struct.
job <Name>: {
    name:     Name
    replicas: 1 | uint
    command:  string
}

job list command: "ls"

job nginx: {
    command:  "nginx"
    replicas: 2
}
```

```
job: {
    list: {
        name:     "list"
        replicas: 1
        command:  "ls"
    }
    nginx: {
        name:     "nginx"
        replicas: 2
        command:  "nginx"
    }
}
```