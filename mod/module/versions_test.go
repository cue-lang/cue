package module_test

import (
	"cuelang.org/go/mod/module"
	"cuelang.org/go/mod/mvs"
)

var _ mvs.Versions[module.Version] = module.Versions{}
