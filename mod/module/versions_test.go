package module_test

import (
	"cuelang.org/go/internal/mod/mvs"
	"cuelang.org/go/mod/module"
)

var _ mvs.Versions[module.Version] = module.Versions{}
