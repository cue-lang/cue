package module_test

import (
	"cuelang.org/go/internal/mod/module"
	"cuelang.org/go/internal/mod/mvs"
)

var _ mvs.Versions[module.Version] = module.Versions{}
