[TOC](Readme.md) [Prev](packages.md) [Next](operators.md)

_Modules, Packages, and Instances_

# Imports

A CUE file may import definitions from builtin or user-defined packages.
A CUE file does not need to be part of a package to use imports.

The example here shows the use of builtin packages.

This code groups the imports into a parenthesized, "factored" import statement.

You can also write multiple import statements, like:

```
import "encoding/json"
import "math"
```

But it is good style to use the factored import statement.

<!-- CUE editor -->
_imports.cue:_
```
import (
	"encoding/json"
	"math"
)

data: json.Marshal({ a: math.Sqrt(7) })
```

<!-- result -->
`$ cue eval imports.cue`
```
data: "{\"a\":2.6457513110645907}"
```