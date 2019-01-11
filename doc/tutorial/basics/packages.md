[TOC](Readme.md) [Prev](instances.md) [Next](imports.md)

_Modules, Packages, and Instances_

# Packages

A CUE file is a standalone file by default.
A `package` clause allows a single configuration to be split across multiple
files.

The configuration for a package is defined by the concatenation of all its
files, after stripping the package clauses and not considering imports.

Duplicate definitions are treated analogously to duplicate definitions within
the same file.
The order in which files are loaded is undefined, but any order will result
in the same outcome, given that order does not matter.

<!-- CUE editor tab 1-->
File a.cue
```
package config

foo: 100
bar: int
```

<!-- CUE editor tab 2-->
File b.cue
```
package config

bar: 200
```

<!-- result -->
Result
```
foo: 100
bar: 200
```
