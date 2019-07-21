[TOC](Readme.md) [Prev](templates.md) [Next](packages.md)

# Modules, Packages, and Instances

## Packages

A CUE file is a standalone file by default.
A `package` clause allows a single configuration to be split across multiple
files.

## Instances

All files within a directory hierarchy with the same package identifier belong
to the same package.
Each directory within this hierarchy provides a different "view" of this package
called an _instance_.
An instance of a package for a directory is defined by all CUE files in that
directory and all of its ancestor directories, belonging to the same package.
This allows common configuration to be shared and policies to be enforced
across a collection of related configurations.

See the [Kubernetes Tutorial](../kubernetes/README.md) for a concrete example
of instances.


## Modules

A _module_ is the directory hierarchy containing the CUE files of a package.
The root of this directory hierarchy is the _module root_.
It may be explicitly marked with a `cue.mod` file.

The module root may contain a `pkg` directory containing packages that are
importable with import.
The first package path component needs to be a domain name, else the cue tool
is unable to import non-core packages.
The convention is to use the URL from which the package is retrieved.

## Example

For importing a package from the `pkg` directory you can create the directory
layout as follows:

```sh
touch cue.mod
mkdir -p pkg/cuelang.org/example
```

In our example the package `cuelang.org` contains the following content.
Note that only identifiers starting with a capital letter may be imported.

_pkg/cuelang.org/example/example.cue:_
```
package example

Foo: 100
```

_a.cue:_
```
package a

import "cuelang.org/example"

bar: example.Foo
```

`$ cue eval a.cue`
```
bar: 100
```
